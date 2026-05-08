package auth

import (
	"context"
	"crypto/hmac"
	cryptoRand "crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"sync"
	"time"

	"shiro-email/backend/internal/modules/system"
	"shiro-email/backend/internal/shared/security"
)

type Service struct {
	repo         Repository
	jwtSecret    string
	configRepo   system.ConfigRepository
	emailSender  EmailSender
	stateMu      sync.Mutex
	oauthByState map[string]oauthStateRecord
}

var ErrRegistrationDisabled = errors.New("registration disabled")
var ErrEmailVerificationRequired = errors.New("email verification required")
var ErrTwoFactorRequired = errors.New("two factor required")
var ErrVerificationAttemptsExceeded = errors.New("verification attempts exceeded")

const maxVerificationAttempts = 5

type PendingVerificationError struct {
	Challenge VerificationChallengeResponse
}

func (e *PendingVerificationError) Error() string {
	return ErrEmailVerificationRequired.Error()
}

type PendingMFAError struct {
	Challenge MFALoginChallengeResponse
}

func (e *PendingMFAError) Error() string {
	return ErrTwoFactorRequired.Error()
}

func NewService(repo Repository, jwtSecret string, options ...any) *Service {
	var resolvedConfigRepo system.ConfigRepository
	var emailSender EmailSender
	for _, option := range options {
		switch current := option.(type) {
		case system.ConfigRepository:
			resolvedConfigRepo = current
		case EmailSender:
			emailSender = current
		}
	}
	if emailSender == nil {
		if _, ok := resolvedConfigRepo.(*system.MemoryConfigRepository); ok {
			emailSender = NewNoopEmailSender()
		} else {
			emailSender = NewConfigSMTPEmailSender(resolvedConfigRepo)
		}
	}
	return &Service{
		repo:         repo,
		jwtSecret:    jwtSecret,
		configRepo:   resolvedConfigRepo,
		emailSender:  emailSender,
		oauthByState: make(map[string]oauthStateRecord),
	}
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (*AuthResponse, error) {
	settings, err := system.LoadAuthRuntimeSettings(ctx, s.configRepo)
	if err != nil {
		return nil, err
	}

	bootstrapAdminRequired, err := s.requiresBootstrapAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if !bootstrapAdminRequired && (!settings.Registration.AllowRegistration || settings.Registration.RegistrationMode == "closed") {
		return nil, ErrRegistrationDisabled
	}

	requiresEmailVerification := settings.Registration.RequireEmailVerification && !bootstrapAdminRequired
	roles := []string{"user"}
	emailVerified := !requiresEmailVerification
	status := "active"
	if requiresEmailVerification {
		status = "pending_verification"
	}
	if bootstrapAdminRequired {
		roles = []string{"admin", "user"}
		emailVerified = true
		status = "active"
	}

	if requiresEmailVerification {
		if err := s.ensureMailDeliveryReady(ctx); err != nil {
			return nil, err
		}
		if pending, err := s.resumePendingRegistration(ctx, req); err != nil {
			return nil, err
		} else if pending != nil {
			return nil, pending
		}
	}

	minPasswordLength := 8
	if settings.Password.MinLength > 0 {
		minPasswordLength = settings.Password.MinLength
	}
	if len(req.Password) < minPasswordLength {
		return nil, fmt.Errorf("password must be at least %d characters", minPasswordLength)
	}

	hash, err := security.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	user, err := s.repo.CreateUser(ctx, User{
		Username:      req.Username,
		Email:         req.Email,
		PasswordHash:  hash,
		Status:        status,
		EmailVerified: emailVerified,
		Roles:         roles,
	})
	if err != nil {
		return nil, err
	}
	if requiresEmailVerification {
		pending, challengeErr := s.issueVerificationChallenge(ctx, user, "register")
		if challengeErr != nil {
			return nil, challengeErr
		}
		return nil, pending
	}
	return s.issueTokens(ctx, user)
}

func (s *Service) ensureMailDeliveryReady(ctx context.Context) error {
	settings, err := system.LoadMailDeliverySettings(ctx, s.configRepo)
	if err != nil {
		return err
	}
	return system.ValidateMailDeliverySettings(settings)
}

func (s *Service) resumePendingRegistration(ctx context.Context, req RegisterRequest) (*PendingVerificationError, error) {
	existing, err := s.repo.FindUserByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if existing.EmailVerified || existing.Status != "pending_verification" {
		return nil, errors.New("email already exists")
	}

	hash, err := security.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}
	user, err := s.repo.RefreshPendingRegistration(ctx, existing.ID, req.Username, hash)
	if err != nil {
		return nil, err
	}
	return s.issueVerificationChallenge(ctx, user, "register")
}

func (s *Service) Login(ctx context.Context, req LoginRequest) (*AuthResponse, error) {
	user, err := s.repo.FindUserByLogin(ctx, req.Login)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}
	if !security.VerifyPassword(user.PasswordHash, req.Password) {
		return nil, errors.New("invalid credentials")
	}
	if user.Status == "banned" || user.Status == "disabled" {
		return nil, errors.New("account suspended")
	}
	if settings, err := system.LoadAuthRuntimeSettings(ctx, s.configRepo); err == nil && settings.Registration.RequireEmailVerification && !user.EmailVerified {
		pending, challengeErr := s.issueVerificationChallenge(ctx, user, "login")
		if challengeErr != nil {
			return nil, challengeErr
		}
		return nil, pending
	}
	if credential, credentialErr := s.repo.FindTOTPCredentialByUserID(ctx, user.ID); credentialErr == nil && credential.Enabled {
		pending, challengeErr := s.issueMFAChallenge(ctx, user.ID)
		if challengeErr != nil {
			return nil, challengeErr
		}
		return nil, pending
	}
	return s.issueTokens(ctx, user)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*AuthResponse, error) {
	hash := HashRefreshToken(refreshToken)
	record, err := s.repo.FindRefreshToken(ctx, hash)
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}
	if record.RevokedAt != nil || time.Now().After(record.ExpiresAt) {
		return nil, errors.New("invalid refresh token")
	}
	user, err := s.repo.FindUserByID(ctx, record.UserID)
	if err != nil {
		return nil, err
	}
	if user.Status == "banned" || user.Status == "disabled" {
		_ = s.repo.RevokeRefreshToken(ctx, hash)
		return nil, errors.New("account suspended")
	}
	if err := s.repo.RevokeRefreshToken(ctx, hash); err != nil {
		return nil, errors.New("invalid refresh token")
	}
	return s.issueTokens(ctx, user)
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	hash := HashRefreshToken(refreshToken)
	return s.repo.RevokeRefreshToken(ctx, hash)
}

func (s *Service) ForgotPassword(ctx context.Context, req ForgotPasswordRequest) (*ForgotPasswordResponse, error) {
	user, err := s.repo.FindUserByLogin(ctx, req.Login)
	if err != nil {
		return nil, errors.New("user not found")
	}
	pending, challengeErr := s.issueCodeChallenge(ctx, user, "password_reset", s.emailSender.SendPasswordResetCode)
	if challengeErr != nil {
		return nil, challengeErr
	}
	return &ForgotPasswordResponse{
		Status:             "ok",
		Email:              pending.Challenge.Email,
		VerificationTicket: pending.Challenge.VerificationTicket,
		ExpiresInSeconds:   pending.Challenge.ExpiresInSeconds,
	}, nil
}

func (s *Service) ResetPassword(ctx context.Context, req ResetPasswordRequest) (*ResetPasswordResponse, error) {
	record, err := s.repo.FindEmailVerificationByTicketHash(ctx, HashRefreshToken(req.VerificationTicket))
	if err != nil {
		return nil, errors.New("invalid verification ticket")
	}
	if record.Purpose != "password_reset" {
		return nil, errors.New("invalid verification ticket")
	}
	if record.ConsumedAt != nil || time.Now().After(record.ExpiresAt) {
		return nil, errors.New("verification code expired")
	}
	if record.Attempts >= maxVerificationAttempts {
		return nil, ErrVerificationAttemptsExceeded
	}
	if !security.VerifyPassword(record.CodeHash, req.Code) {
		_ = s.repo.IncrementEmailVerificationAttempts(ctx, record.ID)
		return nil, errors.New("invalid verification code")
	}

	hash, err := security.HashPassword(req.NewPassword)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateUserPassword(ctx, record.UserID, hash); err != nil {
		return nil, err
	}
	if err := s.repo.ConsumeEmailVerification(ctx, record.ID); err != nil {
		return nil, err
	}
	if err := s.repo.RevokeUserRefreshTokens(ctx, record.UserID); err != nil {
		return nil, err
	}

	return &ResetPasswordResponse{Status: "ok"}, nil
}

func (s *Service) ConfirmEmailVerification(ctx context.Context, req EmailVerificationConfirmRequest) (*AuthResponse, error) {
	record, err := s.repo.FindEmailVerificationByTicketHash(ctx, HashRefreshToken(req.VerificationTicket))
	if err != nil {
		return nil, errors.New("invalid verification ticket")
	}
	if record.ConsumedAt != nil || time.Now().After(record.ExpiresAt) {
		return nil, errors.New("verification code expired")
	}
	if record.Attempts >= maxVerificationAttempts {
		return nil, ErrVerificationAttemptsExceeded
	}
	if !security.VerifyPassword(record.CodeHash, req.Code) {
		_ = s.repo.IncrementEmailVerificationAttempts(ctx, record.ID)
		return nil, errors.New("invalid verification code")
	}
	if err := s.repo.ConsumeEmailVerification(ctx, record.ID); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateUserVerification(ctx, record.UserID, true, "active"); err != nil {
		return nil, err
	}
	user, err := s.repo.FindUserByID(ctx, record.UserID)
	if err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, user)
}

func (s *Service) ResendEmailVerification(ctx context.Context, req EmailVerificationResendRequest) (*VerificationChallengeResponse, error) {
	record, err := s.repo.FindEmailVerificationByTicketHash(ctx, HashRefreshToken(req.VerificationTicket))
	if err != nil {
		return nil, errors.New("invalid verification ticket")
	}
	if record.ConsumedAt != nil {
		return nil, errors.New("verification already completed")
	}
	user, err := s.repo.FindUserByID(ctx, record.UserID)
	if err != nil {
		return nil, err
	}
	pendingErr, challengeErr := s.issueChallengeForPurpose(ctx, user, record.Purpose)
	if challengeErr != nil {
		return nil, challengeErr
	}
	return &pendingErr.Challenge, nil
}

func (s *Service) Settings(ctx context.Context) (AuthSettingsResponse, error) {
	settings, err := system.LoadAuthRuntimeSettings(ctx, s.configRepo)
	if err != nil {
		return AuthSettingsResponse{}, err
	}
	bootstrapAdminRequired, err := s.requiresBootstrapAdmin(ctx)
	if err != nil {
		return AuthSettingsResponse{}, err
	}

	response := AuthSettingsResponse{
		RegistrationMode:         settings.Registration.RegistrationMode,
		AllowRegistration:        settings.Registration.AllowRegistration,
		BootstrapAdminRequired:   bootstrapAdminRequired,
		RequireEmailVerification: settings.Registration.RequireEmailVerification,
		InviteOnly:               settings.Registration.InviteOnly,
		PasswordMinLength:        settings.Password.MinLength,
		AllowMultiSession:        settings.Session.AllowMultiSession,
		RefreshTokenDays:         settings.Session.RefreshTokenDays,
		OAuthShowOnLogin:         settings.OAuthDisplay.ShowOnLogin,
		OAuthProviders:           make(map[string]OAuthProviderSummary, len(settings.OAuth)),
	}
	for provider, item := range settings.OAuth {
		response.OAuthProviders[provider] = OAuthProviderSummary{
			Enabled:           item.Enabled,
			DisplayName:       item.DisplayName,
			RedirectURL:       item.RedirectURL,
			AuthorizationURL:  item.AuthorizationURL,
			Scopes:            append([]string{}, item.Scopes...),
			UsePKCE:           item.UsePKCE,
			AllowAutoRegister: item.AllowAutoRegister,
		}
	}
	return response, nil
}

func (s *Service) GetAccountProfile(ctx context.Context, userID uint64) (AccountProfileResponse, error) {
	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return AccountProfileResponse{}, err
	}

	profile, err := s.getProfileSettings(ctx, user)
	if err != nil {
		return AccountProfileResponse{}, err
	}

	twoFactorEnabled := false
	if credential, credentialErr := s.repo.FindTOTPCredentialByUserID(ctx, userID); credentialErr == nil {
		twoFactorEnabled = credential.Enabled
	}

	return AccountProfileResponse{
		UserID:             user.ID,
		Username:           user.Username,
		Email:              user.Email,
		EmailVerified:      user.EmailVerified,
		Roles:              append([]string{}, user.Roles...),
		DisplayName:        profile.DisplayName,
		Locale:             profile.Locale,
		Timezone:           profile.Timezone,
		AutoRefreshSeconds: profile.AutoRefreshSeconds,
		TwoFactorEnabled:   twoFactorEnabled,
	}, nil
}

func (s *Service) UpdateAccountProfile(ctx context.Context, userID uint64, req UpdateAccountProfileRequest) (AccountProfileResponse, error) {
	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return AccountProfileResponse{}, err
	}

	profile, err := s.getProfileSettings(ctx, user)
	if err != nil {
		return AccountProfileResponse{}, err
	}
	profile.DisplayName = strings.TrimSpace(req.DisplayName)
	profile.Locale = firstNonEmpty(strings.TrimSpace(req.Locale), profile.Locale)
	profile.Timezone = firstNonEmpty(strings.TrimSpace(req.Timezone), profile.Timezone)
	if req.AutoRefreshSeconds > 0 {
		profile.AutoRefreshSeconds = req.AutoRefreshSeconds
	}
	if _, err := s.repo.UpsertProfileSettings(ctx, profile); err != nil {
		return AccountProfileResponse{}, err
	}
	return s.GetAccountProfile(ctx, userID)
}

func (s *Service) ChangePassword(ctx context.Context, userID uint64, req ChangePasswordRequest) error {
	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !security.VerifyPassword(user.PasswordHash, req.CurrentPassword) {
		return errors.New("invalid current password")
	}
	settings, _ := system.LoadAuthRuntimeSettings(ctx, s.configRepo)
	minLen := 8
	if settings.Password.MinLength > 0 {
		minLen = settings.Password.MinLength
	}
	if len(req.NewPassword) < minLen {
		return fmt.Errorf("password must be at least %d characters", minLen)
	}
	hash, err := security.HashPassword(req.NewPassword)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateUserPassword(ctx, userID, hash); err != nil {
		return err
	}
	return s.repo.RevokeUserRefreshTokens(ctx, userID)
}

func (s *Service) GetTOTPStatus(ctx context.Context, userID uint64) (TOTPStatusResponse, error) {
	credential, err := s.repo.FindTOTPCredentialByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return TOTPStatusResponse{Enabled: false}, nil
		}
		return TOTPStatusResponse{}, err
	}
	return TOTPStatusResponse{Enabled: credential.Enabled}, nil
}

func (s *Service) SetupTOTP(ctx context.Context, userID uint64) (SetupTOTPResponse, error) {
	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return SetupTOTPResponse{}, err
	}
	secret, err := generateTOTPSecret()
	if err != nil {
		return SetupTOTPResponse{}, err
	}
	if err := s.repo.UpsertTOTPCredential(ctx, TOTPCredential{
		UserID:           userID,
		SecretCiphertext: secret,
		Enabled:          false,
		VerifiedAt:       nil,
		LastUsedAt:       nil,
	}); err != nil {
		return SetupTOTPResponse{}, err
	}
	return SetupTOTPResponse{
		ManualEntryKey: secret,
		OTPAuthURL:     buildTOTPAuthURL(user, secret),
	}, nil
}

func (s *Service) EnableTOTP(ctx context.Context, userID uint64, req EnableTOTPRequest) error {
	credential, err := s.repo.FindTOTPCredentialByUserID(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := verifyTOTPCode(credential.SecretCiphertext, req.Code, time.Now())
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("invalid totp code")
	}
	now := time.Now()
	credential.Enabled = true
	credential.VerifiedAt = &now
	credential.LastUsedAt = &now
	return s.repo.UpsertTOTPCredential(ctx, credential)
}

func (s *Service) DisableTOTP(ctx context.Context, userID uint64, req DisableTOTPRequest) error {
	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if !security.VerifyPassword(user.PasswordHash, req.Password) {
		return errors.New("invalid current password")
	}
	return s.repo.UpsertTOTPCredential(ctx, TOTPCredential{
		UserID:           userID,
		SecretCiphertext: "",
		Enabled:          false,
		VerifiedAt:       nil,
		LastUsedAt:       nil,
	})
}

func (s *Service) VerifyLoginTOTP(ctx context.Context, req VerifyLoginTOTPRequest) (*AuthResponse, error) {
	record, err := s.repo.FindMFAChallengeByTicketHash(ctx, HashRefreshToken(req.ChallengeTicket))
	if err != nil {
		return nil, errors.New("invalid challenge ticket")
	}
	if record.ConsumedAt != nil || time.Now().After(record.ExpiresAt) {
		return nil, errors.New("mfa challenge expired")
	}
	credential, err := s.repo.FindTOTPCredentialByUserID(ctx, record.UserID)
	if err != nil {
		return nil, err
	}
	if !credential.Enabled {
		return nil, errors.New("two factor not enabled")
	}
	ok, err := verifyTOTPCode(credential.SecretCiphertext, req.Code, time.Now())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("invalid totp code")
	}
	if err := s.repo.ConsumeMFAChallenge(ctx, record.ID); err != nil {
		return nil, err
	}
	now := time.Now()
	credential.LastUsedAt = &now
	if err := s.repo.UpsertTOTPCredential(ctx, credential); err != nil {
		return nil, err
	}
	user, err := s.repo.FindUserByID(ctx, record.UserID)
	if err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, user)
}

func (s *Service) RequestEmailChange(ctx context.Context, userID uint64, req RequestEmailChangeRequest) (*VerificationChallengeResponse, error) {
	user, err := s.repo.FindUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	newEmail := strings.TrimSpace(req.NewEmail)
	if newEmail == "" {
		return nil, errors.New("new email required")
	}
	if existing, lookupErr := s.repo.FindUserByLogin(ctx, newEmail); lookupErr == nil && existing.ID != userID {
		return nil, errors.New("email already exists")
	}

	pending, challengeErr := s.issueCodeChallengeForEmail(ctx, user, newEmail, "change_email", s.emailSender.SendVerificationCode)
	if challengeErr != nil {
		return nil, challengeErr
	}
	return &pending.Challenge, nil
}

func (s *Service) ConfirmEmailChange(ctx context.Context, userID uint64, req ConfirmEmailChangeRequest) (AccountProfileResponse, error) {
	record, err := s.repo.FindEmailVerificationByTicketHash(ctx, HashRefreshToken(req.VerificationTicket))
	if err != nil {
		return AccountProfileResponse{}, errors.New("invalid verification ticket")
	}
	if record.UserID != userID || record.Purpose != "change_email" {
		return AccountProfileResponse{}, errors.New("invalid verification ticket")
	}
	if record.ConsumedAt != nil || time.Now().After(record.ExpiresAt) {
		return AccountProfileResponse{}, errors.New("verification code expired")
	}
	if record.Attempts >= maxVerificationAttempts {
		return AccountProfileResponse{}, ErrVerificationAttemptsExceeded
	}
	if !security.VerifyPassword(record.CodeHash, req.Code) {
		_ = s.repo.IncrementEmailVerificationAttempts(ctx, record.ID)
		return AccountProfileResponse{}, errors.New("invalid verification code")
	}
	if err := s.repo.UpdateUserEmail(ctx, userID, record.Email, true); err != nil {
		return AccountProfileResponse{}, err
	}
	if err := s.repo.ConsumeEmailVerification(ctx, record.ID); err != nil {
		return AccountProfileResponse{}, err
	}
	if err := s.repo.RevokeUserRefreshTokens(ctx, userID); err != nil {
		return AccountProfileResponse{}, err
	}
	return s.GetAccountProfile(ctx, userID)
}

func (s *Service) issueTokens(ctx context.Context, user User) (*AuthResponse, error) {
	settings, err := system.LoadAuthRuntimeSettings(ctx, s.configRepo)
	if err != nil {
		return nil, err
	}

	accessToken, err := security.SignAccessToken(user.ID, user.Roles, s.jwtSecret)
	if err != nil {
		return nil, err
	}
	refreshToken, err := security.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}
	if !settings.Session.AllowMultiSession {
		if err := s.repo.RevokeUserRefreshTokens(ctx, user.ID); err != nil {
			return nil, err
		}
	}
	if err := s.repo.SaveRefreshToken(ctx, RefreshTokenRecord{
		UserID:    user.ID,
		TokenHash: HashRefreshToken(refreshToken),
		ExpiresAt: time.Now().Add(time.Duration(settings.Session.RefreshTokenDays) * 24 * time.Hour),
	}); err != nil {
		return nil, err
	}
	return &AuthResponse{
		Status:       "authenticated",
		UserID:       user.ID,
		Username:     user.Username,
		Roles:        user.Roles,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (s *Service) issueVerificationChallenge(ctx context.Context, user User, purpose string) (*PendingVerificationError, error) {
	return s.issueCodeChallenge(ctx, user, purpose, s.emailSender.SendVerificationCode)
}

func (s *Service) issueChallengeForPurpose(ctx context.Context, user User, purpose string) (*PendingVerificationError, error) {
	if purpose == "password_reset" {
		return s.issueCodeChallenge(ctx, user, purpose, s.emailSender.SendPasswordResetCode)
	}
	return s.issueVerificationChallenge(ctx, user, purpose)
}

func (s *Service) issueCodeChallenge(
	ctx context.Context,
	user User,
	purpose string,
	send func(context.Context, string, string, string) error,
) (*PendingVerificationError, error) {
	ticket, err := security.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}
	code := fmt.Sprintf("%06d", rand.Intn(1000000))
	codeHash, hashErr := security.HashPassword(code)
	if hashErr != nil {
		return nil, hashErr
	}
	expiresAt := time.Now().Add(15 * time.Minute)
	record := EmailVerificationRecord{
		UserID:     user.ID,
		Email:      user.Email,
		Purpose:    purpose,
		TicketHash: HashRefreshToken(ticket),
		CodeHash:   codeHash,
		ExpiresAt:  expiresAt,
		LastSentAt: time.Now(),
	}
	if _, saveErr := s.repo.SaveEmailVerification(ctx, record); saveErr != nil {
		return nil, saveErr
	}
	actionURL := s.buildEmailActionURL(ctx, user, purpose, ticket, user.Email, code)
	if sendErr := send(ctx, user.Email, code, actionURL); sendErr != nil {
		return nil, sendErr
	}
	return &PendingVerificationError{
		Challenge: VerificationChallengeResponse{
			Status:             "verification_required",
			Email:              user.Email,
			VerificationTicket: ticket,
			ExpiresInSeconds:   int(time.Until(expiresAt).Seconds()),
		},
	}, nil
}

func (s *Service) issueCodeChallengeForEmail(
	ctx context.Context,
	user User,
	targetEmail string,
	purpose string,
	send func(context.Context, string, string, string) error,
) (*PendingVerificationError, error) {
	ticket, err := security.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}
	code := fmt.Sprintf("%06d", rand.Intn(1000000))
	codeHash, hashErr := security.HashPassword(code)
	if hashErr != nil {
		return nil, hashErr
	}
	expiresAt := time.Now().Add(15 * time.Minute)
	record := EmailVerificationRecord{
		UserID:     user.ID,
		Email:      targetEmail,
		Purpose:    purpose,
		TicketHash: HashRefreshToken(ticket),
		CodeHash:   codeHash,
		ExpiresAt:  expiresAt,
		LastSentAt: time.Now(),
	}
	if _, saveErr := s.repo.SaveEmailVerification(ctx, record); saveErr != nil {
		return nil, saveErr
	}
	actionURL := s.buildEmailActionURL(ctx, user, purpose, ticket, targetEmail, code)
	if sendErr := send(ctx, targetEmail, code, actionURL); sendErr != nil {
		return nil, sendErr
	}
	return &PendingVerificationError{
		Challenge: VerificationChallengeResponse{
			Status:             "verification_required",
			Email:              targetEmail,
			VerificationTicket: ticket,
			ExpiresInSeconds:   int(time.Until(expiresAt).Seconds()),
		},
	}, nil
}

func (s *Service) issueMFAChallenge(ctx context.Context, userID uint64) (*PendingMFAError, error) {
	ticket, err := security.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(5 * time.Minute)
	record := MFAChallengeRecord{
		UserID:     userID,
		TicketHash: HashRefreshToken(ticket),
		Purpose:    "login_totp",
		ExpiresAt:  expiresAt,
	}
	if _, err := s.repo.SaveMFAChallenge(ctx, record); err != nil {
		return nil, err
	}
	return &PendingMFAError{
		Challenge: MFALoginChallengeResponse{
			Status:           "two_factor_required",
			ChallengeTicket:  ticket,
			ExpiresInSeconds: int(time.Until(expiresAt).Seconds()),
		},
	}, nil
}

func (s *Service) getProfileSettings(ctx context.Context, user User) (ProfileSettings, error) {
	profile, err := s.repo.GetProfileSettings(ctx, user.ID)
	if err == nil {
		expectedDisplayName := defaultAccountDisplayName(user)
		if strings.TrimSpace(profile.DisplayName) == "" || strings.TrimSpace(profile.DisplayName) == "GALA Workspace" {
			profile.DisplayName = expectedDisplayName
			if profile.Locale == "" {
				profile.Locale = "zh-CN"
			}
			if profile.Timezone == "" {
				profile.Timezone = "Asia/Shanghai"
			}
			if profile.AutoRefreshSeconds <= 0 {
				profile.AutoRefreshSeconds = 30
			}
			return s.repo.UpsertProfileSettings(ctx, profile)
		}
		return profile, nil
	}
	if !errors.Is(err, ErrUserNotFound) {
		return ProfileSettings{}, err
	}

	return s.repo.UpsertProfileSettings(ctx, ProfileSettings{
		UserID:             user.ID,
		DisplayName:        defaultAccountDisplayName(user),
		Locale:             "zh-CN",
		Timezone:           "Asia/Shanghai",
		AutoRefreshSeconds: 30,
	})
}

func defaultAccountDisplayName(user User) string {
	displayName := strings.TrimSpace(user.Username)
	if displayName == "" {
		displayName = strings.TrimSpace(user.Email)
	}
	if at := strings.Index(displayName, "@"); at > 0 {
		displayName = strings.TrimSpace(displayName[:at])
	}
	if displayName == "" {
		displayName = "user"
	}
	return displayName
}

func (s *Service) requiresBootstrapAdmin(ctx context.Context) (bool, error) {
	users, err := s.repo.ListUsers(ctx)
	if err != nil {
		return false, err
	}
	for _, user := range users {
		for _, role := range user.Roles {
			if strings.EqualFold(strings.TrimSpace(role), "admin") {
				return false, nil
			}
		}
	}
	return true, nil
}

func (s *Service) buildEmailActionURL(ctx context.Context, user User, purpose string, ticket string, email string, code string) string {
	baseURL := "http://localhost:5173"
	if site, err := system.LoadPublicSiteSettings(ctx, s.configRepo); err == nil {
		if resolved := strings.TrimSpace(site.Identity.AppBaseURL); resolved != "" {
			baseURL = strings.TrimRight(resolved, "/")
		}
	}

	query := url.Values{}
	query.Set("ticket", ticket)
	if trimmedEmail := strings.TrimSpace(email); trimmedEmail != "" {
		query.Set("email", trimmedEmail)
	}
	if trimmedCode := strings.TrimSpace(code); trimmedCode != "" {
		query.Set("code", trimmedCode)
	}

	switch purpose {
	case "register", "login":
		return baseURL + "/auth/verify-email?" + query.Encode()
	case "password_reset":
		return baseURL + "/auth/reset-password?" + query.Encode()
	case "change_email":
		accountPath := "/dashboard/settings"
		for _, role := range user.Roles {
			if role == "admin" {
				accountPath = "/admin/account"
				break
			}
		}
		actionQuery := url.Values{}
		actionQuery.Set("action", "change-email")
		actionQuery.Set("emailChangeTicket", ticket)
		if trimmedEmail := strings.TrimSpace(email); trimmedEmail != "" {
			actionQuery.Set("emailChangeEmail", trimmedEmail)
		}
		if trimmedCode := strings.TrimSpace(code); trimmedCode != "" {
			actionQuery.Set("emailChangeCode", trimmedCode)
		}
		return baseURL + accountPath + "?" + actionQuery.Encode()
	default:
		return ""
	}
}

func generateTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := cryptoRand.Read(buf); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

func buildTOTPAuthURL(user User, secret string) string {
	issuer := "Shiro Email"
	label := issuer + ":" + user.Email
	return "otpauth://totp/" + url.PathEscape(label) +
		"?secret=" + url.QueryEscape(secret) +
		"&issuer=" + url.QueryEscape(issuer)
}

func generateTOTPCodeAt(secret string, at time.Time) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}

	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	mac := hmac.New(sha1.New, key)
	if _, err := mac.Write(msg[:]); err != nil {
		return "", err
	}
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (int(sum[offset])&0x7f)<<24 |
		(int(sum[offset+1])&0xff)<<16 |
		(int(sum[offset+2])&0xff)<<8 |
		(int(sum[offset+3]) & 0xff)
	return fmt.Sprintf("%06d", code%1000000), nil
}

func verifyTOTPCode(secret string, code string, now time.Time) (bool, error) {
	for _, window := range []int{-30, 0, 30} {
		candidate, err := generateTOTPCodeAt(secret, now.Add(time.Duration(window)*time.Second))
		if err != nil {
			return false, err
		}
		if candidate == strings.TrimSpace(code) {
			return true, nil
		}
	}
	return false, nil
}
