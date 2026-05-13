import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  fetchAuthSettings,
  login,
  register,
  resendEmailVerification,
  requestPasswordReset,
  resetPassword,
  startOAuthLogin,
  verifyLoginTOTP,
} from "../../features/auth/api";
import { fetchPublicSiteSettings } from "../../features/home/api";
import { LoginModal } from "./login-modal";

vi.mock("../../features/auth/api", () => ({
  fetchAuthSettings: vi.fn(),
  login: vi.fn(),
  register: vi.fn(),
  resendEmailVerification: vi.fn(),
  requestPasswordReset: vi.fn(),
  resetPassword: vi.fn(),
  startOAuthLogin: vi.fn(),
  verifyLoginTOTP: vi.fn(),
}));

vi.mock("../../features/home/api", () => ({
  fetchPublicSiteSettings: vi.fn(),
}));

describe("LoginModal", () => {
  beforeEach(() => {
    cleanup();
    vi.clearAllMocks();

    vi.mocked(fetchAuthSettings).mockResolvedValue({
      registrationMode: "invite_only",
      allowRegistration: true,
      requireEmailVerification: true,
      inviteOnly: true,
      passwordMinLength: 8,
      allowMultiSession: true,
      refreshTokenDays: 7,
      oauthShowOnLogin: true,
      oauthProviders: {
        google: {
          enabled: true,
          displayName: "Google",
          redirectUrl: "",
          authorizationUrl: "https://accounts.google.com/test-auth",
          scopes: ["openid", "email"],
          usePkce: true,
          allowAutoRegister: true,
        },
        github: {
          enabled: false,
          displayName: "GitHub",
          redirectUrl: "",
          authorizationUrl: "",
          scopes: ["read:user"],
          usePkce: true,
          allowAutoRegister: true,
        },
      },
    });
    vi.mocked(login).mockRejectedValue(new Error("not used in this test"));
    vi.mocked(register).mockResolvedValue({
      kind: "session",
      session: {
        accessToken: "token",
        refreshToken: "refresh",
        user: {
          userId: 7,
          username: "new-user",
          roles: ["user"],
        },
      },
    });
    vi.mocked(requestPasswordReset).mockResolvedValue({
      status: "ok",
      email: "new-user@example.com",
      verificationTicket: "ticket-123",
      expiresInSeconds: 900,
    });
    vi.mocked(resendEmailVerification).mockResolvedValue({
      status: "verification_required",
      email: "new-user@example.com",
      verificationTicket: "ticket-456",
      expiresInSeconds: 900,
    });
    vi.mocked(resetPassword).mockResolvedValue({
      status: "ok",
    });
    vi.mocked(startOAuthLogin).mockResolvedValue({
      provider: "google",
      authorizationUrl: "https://accounts.google.com/test-auth",
    });
    vi.mocked(verifyLoginTOTP).mockResolvedValue({
      accessToken: "token-2fa",
      refreshToken: "refresh-2fa",
      user: {
        userId: 7,
        username: "new-user",
        roles: ["user"],
      },
    });
    vi.mocked(fetchPublicSiteSettings).mockResolvedValue({
      identity: {
        siteName: "Shiro Email",
        slogan: "Enterprise temporary mail platform",
        supportEmail: "support@shiro.local",
        siteIconUrl: "",
        appBaseUrl: "http://localhost:5173",
        defaultLanguage: "zh-CN",
        defaultTimeZone: "Asia/Shanghai",
        ambientThemeEnabled: true,
        ambientThemeIntensity: "balanced",
      },
      mailDns: {
        mxTarget: "mail.shiro.local",
        dkimCnameTarget: "shiro._domainkey.shiro.local",
      },
    });
  });

  function renderModal() {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <LoginModal onOpenChange={() => {}} open />
        </QueryClientProvider>
      </MemoryRouter>,
    );
  }

  it("renders auth policy and enabled oauth providers from auth settings", async () => {
    renderModal();

    expect(
      await screen.findByText("当前站点仅支持邀请码注册"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "新账号需要完成邮箱验证后才能激活",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "继续使用 Google" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "创建账号" }),
    ).toBeEnabled();
  });

  it("starts oauth login when clicking provider button", async () => {
    const assignSpy = vi.fn();
    Object.defineProperty(window, "location", {
      value: { assign: assignSpy },
      writable: true,
    });

    renderModal();

    fireEvent.click(
      await screen.findByRole("button", { name: "继续使用 Google" }),
    );

    await waitFor(() => {
      expect(vi.mocked(startOAuthLogin)).toHaveBeenCalledWith("google");
      expect(assignSpy).toHaveBeenCalledWith(
        "https://accounts.google.com/test-auth",
      );
    });
  });

  it("switches to register mode and submits register request", async () => {
    renderModal();

    await screen.findByText("当前站点仅支持邀请码注册");
    const createAccountButton = screen.getByRole("button", {
      name: "创建账号",
    });
    await waitFor(() => {
      expect(createAccountButton).toBeEnabled();
    });
    fireEvent.click(createAccountButton);

    await screen.findByText("创建账号 · Shiro Email");

    fireEvent.change(screen.getByLabelText("用户名"), {
      target: { value: "new-user" },
    });
    fireEvent.change(screen.getByLabelText("邮箱"), {
      target: { value: "new-user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "Secret123!" },
    });

    fireEvent.click(
      screen.getByRole("button", {
        name: "创建并进入工作台",
      }),
    );

    await waitFor(() => {
      expect(vi.mocked(register)).toHaveBeenCalledWith({
        username: "new-user",
        email: "new-user@example.com",
        password: "Secret123!",
      });
    });
  });

  it("requests an email reset code and submits a password reset", async () => {
    renderModal();

    await screen.findByText("当前站点仅支持邀请码注册");
    fireEvent.click(screen.getByRole("button", { name: "忘记密码" }));

    await screen.findByText("重置密码");

    fireEvent.change(screen.getByLabelText("账号"), {
      target: { value: "new-user@example.com" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "发送验证码" }),
    );

    await waitFor(() => {
      expect(vi.mocked(requestPasswordReset)).toHaveBeenCalledWith({
        login: "new-user@example.com",
      });
    });

    expect(
      await screen.findByText(
        "验证码已发送到你的账户邮箱，请输入验证码和新密码完成重置。",
      ),
    ).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("验证码"), {
      target: { value: "123456" },
    });
    fireEvent.change(screen.getByLabelText("新密码"), {
      target: { value: "BetterSecret456!" },
    });
    fireEvent.click(screen.getByRole("button", { name: "重置密码" }));

    await waitFor(() => {
      expect(vi.mocked(resetPassword)).toHaveBeenCalledWith({
        verificationTicket: "ticket-123",
        code: "123456",
        newPassword: "BetterSecret456!",
      });
    });
  });

  it("resends reset verification code from reset mode", async () => {
    renderModal();

    await screen.findByText("当前站点仅支持邀请码注册");
    fireEvent.click(screen.getByRole("button", { name: "忘记密码" }));
    fireEvent.change(screen.getByLabelText("账号"), {
      target: { value: "new-user@example.com" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "发送验证码" }),
    );

    await screen.findByText("验证码已发送至 new-user@example.com");
    fireEvent.click(screen.getByRole("button", { name: "重新发送验证码" }));

    await waitFor(() => {
      expect(vi.mocked(resendEmailVerification)).toHaveBeenCalledWith({
        verificationTicket: "ticket-123",
      });
      expect(vi.mocked(resetPassword)).not.toHaveBeenCalled();
    });
  });

  it("shows a second-step totp form when login requires two factor", async () => {
    vi.mocked(login).mockResolvedValue({
      kind: "two_factor_required",
      challenge: {
        status: "two_factor_required",
        challengeTicket: "mfa-ticket",
        expiresInSeconds: 300,
      },
    });

    renderModal();

    await screen.findByText("当前站点仅支持邀请码注册");
    fireEvent.change(screen.getByLabelText("账号"), {
      target: { value: "new-user@example.com" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "Secret123!" },
    });
    fireEvent.click(screen.getByRole("button", { name: "登录" }));

    expect(await screen.findByText("两步验证")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("验证码"), {
      target: { value: "123456" },
    });
    fireEvent.click(screen.getByRole("button", { name: "验证并继续" }));

    await waitFor(() => {
      expect(vi.mocked(verifyLoginTOTP)).toHaveBeenCalledWith({
        challengeTicket: "mfa-ticket",
        code: "123456",
      });
    });
  });

  it("clears login form state after closing and reopening", async () => {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });

    const { rerender } = render(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <LoginModal onOpenChange={() => {}} open />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    await screen.findByText("当前站点仅支持邀请码注册");
    fireEvent.change(screen.getByLabelText("账号"), {
      target: { value: "admin@example.com" },
    });
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "Secret123!" },
    });

    rerender(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <LoginModal onOpenChange={() => {}} open={false} />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    rerender(
      <MemoryRouter>
        <QueryClientProvider client={queryClient}>
          <LoginModal onOpenChange={() => {}} open />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    expect((await screen.findByLabelText("账号"))).toHaveValue("");
    expect(screen.getByLabelText("密码")).toHaveValue("");
  });
});
