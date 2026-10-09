package middleware

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Admit a subscription request before calling any downstream gateway handler.
// The lease also fences stale subscription cache entries after conversion.
func nextWithSubscriptionConversionLease(c *gin.Context, subscriptions *service.SubscriptionService, subscription *service.UserSubscription, userID int64, googleStyle bool, next func()) {
	if subscriptions == nil || subscription == nil {
		next()
		return
	}
	lease, err := subscriptions.AcquireSubscriptionConversionLease(c.Request.Context(), subscription.ID, userID)
	if err != nil {
		status, message := http.StatusServiceUnavailable, "Subscription admission is temporarily unavailable"
		if errors.Is(err, service.ErrConversionUnavailable) {
			status, message = http.StatusForbidden, "No active subscription found for this group"
		}
		if googleStyle {
			abortWithGoogleError(c, status, message)
		} else {
			AbortWithError(c, status, "SUBSCRIPTION_UNAVAILABLE", message)
		}
		return
	}
	defer lease.Release()
	defer func() {
		if recovered := recover(); recovered != nil {
			lease.MarkUnsettled()
			panic(recovered)
		}
	}()
	c.Request = c.Request.WithContext(service.WithSubscriptionConversionLease(c.Request.Context(), lease))
	next()
}
