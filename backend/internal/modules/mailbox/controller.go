package mailbox

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"shiro-email/backend/internal/middleware"
	"shiro-email/backend/internal/modules/domain"
	"shiro-email/backend/internal/modules/portal"
)

type Controller struct {
	service *Service
}

func NewController(service *Service) *Controller {
	return &Controller{service: service}
}

func (c *Controller) Dashboard(ctx *gin.Context) {
	userID, ok := currentUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}

	payload, err := c.service.BuildDashboard(ctx, userID, currentAPIKeyArgs(ctx)...)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "failed to build dashboard"})
		return
	}
	ctx.JSON(http.StatusOK, payload)
}

func (c *Controller) List(ctx *gin.Context) {
	userID, ok := currentUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}

	items, err := c.service.ListMailboxes(ctx, userID, currentAPIKeyArgs(ctx)...)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "failed to list mailboxes"})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"items": items})
}

func (c *Controller) Create(ctx *gin.Context) {
	userID, ok := currentUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}

	var req CreateMailboxRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "invalid request"})
		return
	}

	item, err := c.service.CreateMailbox(ctx, userID, req, currentAPIKeyArgs(ctx)...)
	if err != nil {
		switch {
		case errors.Is(err, portal.ErrAPIKeyForbidden):
			ctx.JSON(http.StatusForbidden, gin.H{"message": "forbidden"})
		case errors.Is(err, ErrInvalidMailboxTTL):
			ctx.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		case errors.Is(err, ErrInvalidLocalPart):
			ctx.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		case errors.Is(err, ErrDomainVerificationRequired):
			ctx.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		case errors.Is(err, domain.ErrDomainNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{"message": "failed to create mailbox"})
		}
		return
	}

	ctx.JSON(http.StatusCreated, item)
}

func (c *Controller) Extend(ctx *gin.Context) {
	c.updateExpiry(ctx)
}

func (c *Controller) Release(ctx *gin.Context) {
	userID, ok := currentUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}

	mailboxID, ok := mailboxIDFromParam(ctx)
	if !ok {
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "invalid mailbox id"})
		return
	}

	item, err := c.service.ReleaseMailbox(ctx, userID, mailboxID, currentAPIKeyArgs(ctx)...)
	if err != nil {
		if errors.Is(err, portal.ErrAPIKeyForbidden) {
			ctx.JSON(http.StatusForbidden, gin.H{"message": "forbidden"})
			return
		}
		if errors.Is(err, ErrMailboxNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "failed to release mailbox"})
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func (c *Controller) UpdateForwarding(ctx *gin.Context) {
	userID, ok := currentUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}

	mailboxID, ok := mailboxIDFromParam(ctx)
	if !ok {
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "invalid mailbox id"})
		return
	}

	var req UpdateForwardingRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "invalid request"})
		return
	}

	item, err := c.service.UpdateForwarding(ctx, userID, mailboxID, req)
	if err != nil {
		if errors.Is(err, ErrMailboxNotFound) {
			ctx.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
			return
		}
		ctx.JSON(http.StatusInternalServerError, gin.H{"message": "failed to update forwarding"})
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func (c *Controller) updateExpiry(ctx *gin.Context) {
	userID, ok := currentUserID(ctx)
	if !ok {
		ctx.JSON(http.StatusUnauthorized, gin.H{"message": "unauthorized"})
		return
	}

	mailboxID, ok := mailboxIDFromParam(ctx)
	if !ok {
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "invalid mailbox id"})
		return
	}

	var req ExtendMailboxRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"message": "invalid request"})
		return
	}

	item, err := c.service.ExtendMailbox(ctx, userID, mailboxID, req, currentAPIKeyArgs(ctx)...)
	if err != nil {
		switch {
		case errors.Is(err, portal.ErrAPIKeyForbidden):
			ctx.JSON(http.StatusForbidden, gin.H{"message": "forbidden"})
		case errors.Is(err, ErrInvalidMailboxTTL):
			ctx.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		case errors.Is(err, ErrMailboxNotFound):
			ctx.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{"message": "failed to extend mailbox"})
		}
		return
	}
	ctx.JSON(http.StatusOK, item)
}

func currentUserID(ctx *gin.Context) (uint64, bool) {
	value, exists := ctx.Get("auth.userID")
	if !exists {
		return 0, false
	}
	userID, ok := value.(uint64)
	return userID, ok
}

func mailboxIDFromParam(ctx *gin.Context) (uint64, bool) {
	value := ctx.Param("mailboxId")
	if value == "" {
		value = ctx.Param("id")
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

func currentAPIKeyArgs(ctx *gin.Context) []portal.APIKey {
	apiKey, ok := middleware.CurrentAPIKey(ctx)
	if !ok {
		return nil
	}
	return []portal.APIKey{apiKey}
}
