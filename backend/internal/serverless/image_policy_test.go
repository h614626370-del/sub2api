package serverless

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/imagepolicy"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func legacyForwardProof(m *Manager, t ticket, r *http.Request) string {
	return m.mac("forward", t.Node, t.Boot, t.IP, strconv.FormatInt(t.At, 10), t.Nonce, r.Method, r.URL.RequestURI(),
		r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"), r.Header.Get("X-Goog-Api-Key"))
}

func TestServerlessImagePolicyForwardAndRestore(t *testing.T) {
	cache := cacheFor(t)
	store := &memoryStore{}
	node := managerFor(t, "us-pod", cache, store)
	router := gin.New()
	router.Use(node.Ingress())
	var received bool
	router.POST("/v1/images/generations", func(c *gin.Context) {
		received = imagepolicy.DirectOnly(c.Request.Context())
		c.Status(200)
	})
	srv := httptest.NewServer(router)
	defer srv.Close()
	origin := managerFor(t, "", cache, store)
	for _, direct := range []bool{false, true} {
		req := testRequest("POST", "/v1/images/generations", nil)
		req.Header.Set("Authorization", "Bearer fake-user-key")
		if direct {
			req = req.WithContext(imagepolicy.WithDirectOnly(req.Context()))
		}
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = req
		origin.forward(c, Pod{ID: "us-pod", Boot: node.boot, Endpoint: srv.URL}, "203.0.113.4")
		c.Writer.WriteHeaderNow()
		require.Equal(t, 200, rec.Code)
		require.Equal(t, direct, received)
	}
}

func TestServerlessImagePolicySignaturesCannotBeDowngraded(t *testing.T) {
	node := managerFor(t, "us-pod", cacheFor(t), &memoryStore{})
	for _, test := range []struct {
		name   string
		direct bool
		mutate func(*ticket)
		want   int
	}{
		{"direct", true, func(*ticket) {}, 200},
		{"ordinary", false, func(*ticket) {}, 200},
		{"strip policy", true, func(t *ticket) { t.ImagesDirectOnly = false }, 403},
		{"inject policy", false, func(t *ticket) { t.ImagesDirectOnly = true }, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := gin.New()
			r.Use(node.Ingress())
			r.POST("/v1/images/edits", func(c *gin.Context) {
				require.Equal(t, test.direct, imagepolicy.DirectOnly(c.Request.Context()))
				c.Status(200)
			})
			req := testRequest("POST", "/v1/images/edits", nil)
			ticket := ticket{Node: "us-pod", Boot: node.boot, IP: "203.0.113.4", At: time.Now().Unix(), Nonce: randomID(), ImagesDirectOnly: test.direct}
			ticket.Sig = node.proof(ticket, req)
			if test.direct {
				require.NotEqual(t, legacyForwardProof(node, ticket, req), ticket.Sig)
			} else {
				require.Equal(t, legacyForwardProof(node, ticket, req), ticket.Sig)
			}
			test.mutate(&ticket)
			raw, err := json.Marshal(ticket)
			require.NoError(t, err)
			req.Header.Set(forwardHeader, string(raw))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, test.want, rec.Code)
		})
	}
}

func TestServerlessImagePolicyRejectedByLegacyNode(t *testing.T) {
	origin := managerFor(t, "", cacheFor(t), &memoryStore{})
	node := managerFor(t, "old-node", origin.redis, &memoryStore{})
	hits := 0
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ticket ticket
		if json.Unmarshal([]byte(r.Header.Get(forwardHeader)), &ticket) != nil || ticket.Sig != legacyForwardProof(node, ticket, r) {
			w.WriteHeader(403)
			return
		}
		hits++
		w.WriteHeader(200)
	}))
	defer old.Close()
	req := testRequest("POST", "/v1/images/generations", nil)
	req = req.WithContext(imagepolicy.WithDirectOnly(req.Context()))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	origin.forward(c, Pod{ID: "old-node", Boot: node.boot, Endpoint: old.URL}, "203.0.113.4")
	c.Writer.WriteHeaderNow()
	require.Equal(t, 403, rec.Code)
	require.Zero(t, hits)
}

func TestServerlessDoesNotTrustUnsignedImagePolicyHeaders(t *testing.T) {
	node := managerFor(t, "us-pod", cacheFor(t), &memoryStore{})
	r := gin.New()
	r.Use(node.Ingress())
	r.POST("/v1/images/generations", func(c *gin.Context) {
		require.False(t, imagepolicy.DirectOnly(c.Request.Context()))
		c.Status(200)
	})
	req := testRequest("POST", "/v1/images/generations", nil)
	req.Header.Set("X-Sub2api-Images-Direct-Only", "true")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
}

func TestServerlessImagePolicyRejectsNonImageEndpoint(t *testing.T) {
	node := managerFor(t, "us-pod", cacheFor(t), &memoryStore{})
	r := gin.New()
	r.Use(node.Ingress())
	r.POST("/v1/responses", func(c *gin.Context) { t.Error("must not dispatch") })
	req := testRequest("POST", "/v1/responses", nil)
	ticket := ticket{Node: "us-pod", Boot: node.boot, IP: "203.0.113.4",
		At: time.Now().Unix(), Nonce: randomID(), ImagesDirectOnly: true}
	ticket.Sig = node.proof(ticket, req)
	raw, err := json.Marshal(ticket)
	require.NoError(t, err)
	req.Header.Set(forwardHeader, string(raw))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, 403, rec.Code)
}
