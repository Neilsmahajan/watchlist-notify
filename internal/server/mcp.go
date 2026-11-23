package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/neilsmahajan/watchlist-notify/internal/middleware"
)

// This file puts a Model Context Protocol server on the API: one endpoint,
// POST /mcp, through which an AI client reads and edits the watchlist of the
// user who authorized it. The tools it serves are in mcp_tools.go and the
// token checks in internal/middleware/mcp.go; docs/MCP.md describes the whole
// from the outside.
//
// The endpoint is stateless. A request carries everything needed to answer
// it, there are no sessions, and every request is authorized on its own, so
// any instance can serve any request and nothing is kept in memory between
// two of them but the caches the REST API already has.

const (
	// mcpPath is where the endpoint is served. The resource URL must end in
	// it: the URL identifies this endpoint and no other.
	mcpPath = "/mcp"
	// mcpMetadataPath is where a client that got a 401 finds out which
	// authorization server to use (RFC 9728). A resource with a path has its
	// metadata at the well-known prefix followed by that path; the bare
	// prefix is served too, for clients that only try the root.
	mcpMetadataPath = "/.well-known/oauth-protected-resource"

	// mcpMaxBodyBytes caps a request body. A tool call is a few hundred
	// bytes; the cap is there so that nobody can make the server buffer more.
	mcpMaxBodyBytes = 1 << 20
	// mcpToolTimeout bounds one tool call, database and TMDb time included.
	// It leaves room inside the HTTP server's 30 s write timeout for the
	// answer to be written.
	mcpToolTimeout = 20 * time.Second
)

// mcpConfig is what the MCP endpoint needs to know about its deployment.
type mcpConfig struct {
	// resourceURL identifies the endpoint: it is the "resource" a client asks
	// the authorization server for and the audience a token must carry.
	resourceURL string
	// metadataURL is where the protected resource metadata is served.
	metadataURL string
	// issuer is the authorization server, exactly as it names itself in the
	// "iss" claim of its tokens.
	issuer *url.URL
	// jwksOptions reach the JWKS provider; tests use them.
	jwksOptions []any
}

// loadMCPConfig reads the MCP configuration. With no resource URL the
// endpoint is off and the result is nil. A resource URL that cannot be used
// is an error rather than a reason to carry on without the endpoint: the
// operator asked for it, and a typo here would otherwise show up as tokens
// that never validate.
func loadMCPConfig(resourceURL, auth0Domain string) (*mcpConfig, error) {
	resourceURL = strings.TrimSpace(resourceURL)
	if resourceURL == "" {
		return nil, nil
	}
	u, err := url.Parse(resourceURL)
	switch {
	case err != nil:
		return nil, fmt.Errorf("MCP_RESOURCE_URL: %w", err)
	case (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil:
		return nil, errors.New("MCP_RESOURCE_URL must be an absolute http(s) URL")
	case u.Path != mcpPath:
		return nil, fmt.Errorf("MCP_RESOURCE_URL must end in %s, the path the endpoint is served on", mcpPath)
	case u.RawQuery != "" || u.Fragment != "" || strings.HasSuffix(resourceURL, "?") || strings.HasSuffix(resourceURL, "#"):
		return nil, errors.New("MCP_RESOURCE_URL must not have a query or a fragment")
	}
	issuer, err := middleware.Auth0Issuer(auth0Domain)
	if err != nil {
		return nil, fmt.Errorf("MCP_RESOURCE_URL is set but the authorization server is not: %w", err)
	}
	origin := u.Scheme + "://" + u.Host
	return &mcpConfig{
		resourceURL: resourceURL,
		metadataURL: origin + mcpMetadataPath + mcpPath,
		issuer:      issuer,
	}, nil
}

// origin returns the origin the endpoint itself is served from.
func (cfg *mcpConfig) origin() string {
	origin, _ := originOf(cfg.resourceURL)
	return origin
}

// isMCPPath reports whether path belongs to the MCP endpoint or its
// metadata, which the API's gzip and CORS middleware must leave alone.
func isMCPPath(path string) bool {
	return path == mcpPath || path == mcpMetadataPath || path == mcpMetadataPath+mcpPath
}

// exceptMCP runs a middleware for every request but those to the MCP
// endpoint and its metadata.
//
// Two of the API's global middlewares would be wrong there. The CORS
// middleware allows credentials and answers any origin it does not know with
// a 403, while the metadata must be readable from every origin and the
// endpoint has its own, cookie-free, policy. The gzip middleware buffers what
// it compresses, which would hold back a streamed response if the endpoint
// ever sent one. Skipping them by path, here, says so in one place instead of
// leaving it to the order routes happen to be registered in.
func exceptMCP(mw gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if isMCPPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		mw(c)
	}
}

// withoutMCPQuery drops the query string of requests to the MCP endpoint
// before anything sees it. The endpoint takes no parameters, and the access
// log prints the query: a client that wrongly put its token there
// (?access_token=...) would otherwise have it written to the log.
//
// The path is compared without trailing slashes. The router answers /mcp/
// with a redirect to /mcp that repeats the query, and in debug mode prints
// the redirect, so that spelling must lose its query as well.
func withoutMCPQuery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMCPPath(strings.TrimRight(r.URL.Path, "/")) && r.URL.RawQuery != "" {
			r.URL.RawQuery = ""
			r.RequestURI = r.URL.RequestURI()
		}
		next.ServeHTTP(w, r)
	})
}

// registerMCP adds the MCP endpoint and its metadata documents to the router.
// frontendOrigins are the browser origins the REST API accepts.
func (s *Server) registerMCP(r *gin.Engine, cfg *mcpConfig, frontendOrigins []string) error {
	guard, err := middleware.NewMCPAuth(middleware.MCPAuthConfig{
		Issuer:      cfg.issuer,
		ResourceURL: cfg.resourceURL,
		MetadataURL: cfg.metadataURL,
		JWKSOptions: cfg.jwksOptions,
	})
	if err != nil {
		return err
	}

	// A page served by the API's own origin is as trusted as the frontend.
	origins := append([]string{cfg.origin()}, frontendOrigins...)
	// Any, not POST: the endpoint answers the other methods itself, with the
	// 405 the transport specifies, and a browser's preflight must reach it.
	r.Any(mcpPath, gin.WrapH(s.mcpEndpoint(guard, origins)))

	metadata := gin.WrapH(mcpMetadataHandler(cfg))
	r.Any(mcpMetadataPath+mcpPath, metadata)
	r.Any(mcpMetadataPath, metadata)
	return nil
}

// mcpMetadataHandler serves the protected resource metadata (RFC 9728): what
// this resource is called, which authorization server issues tokens for it
// and which scopes exist. It is public, and the SDK's handler lets a page on
// any origin read it, without credentials.
func mcpMetadataHandler(cfg *mcpConfig) http.Handler {
	document := auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               cfg.resourceURL,
		AuthorizationServers:   []string{cfg.issuer.String()},
		ScopesSupported:        middleware.MCPScopes,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "Watchlist Notify",
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// It changes only with a deploy.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		document.ServeHTTP(w, r)
	})
}

// mcpEndpoint builds the handler behind POST /mcp. A request passes, in
// order:
//
//  1. the origin check: a browser page may call the endpoint only from an
//     allowed origin; a request with no Origin header is not from a page and
//     passes;
//  2. the method check: anything but POST is a 405. There is no stream to
//     open with GET and no session to end with DELETE;
//  3. the token check: 401, or 403 without the read scope;
//  4. the reserved-account check;
//  5. the scope check for the tool being called;
//  6. the SDK's transport, which runs the tool.
//
// The method check comes before the token check on purpose. Which methods
// exist is not a secret, and a client probing with GET for a stream should
// learn that there is none, not be sent to sign in for something that will
// never work.
func (s *Server) mcpEndpoint(guard *middleware.MCPAuth, origins []string) http.Handler {
	transport, scopes := s.mcpTransport()

	h := requireToolScopes(guard, scopes, transport)
	h = refuseReservedAccount(h)
	h = guard.RequireToken(h)
	h = postOnly(h)
	return middleware.AllowOrigins(origins, h)
}

// mcpTransport returns the SDK's Streamable HTTP handler serving the tools,
// and the scope each tool needs. It expects a verified token in the request's
// context; see mcpEndpoint for what goes in front of it.
func (s *Server) mcpTransport() (http.Handler, map[string]string) {
	server, scopes := s.newMCPServer()

	transport := mcp.NewStreamableHTTPHandler(
		// One server for every request: it holds the tools and no state.
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless: true,
			// Answer with one JSON document, not an event stream. Every tool
			// here returns a single result, so there is nothing to stream, and
			// a plain response cannot be held back by a buffering proxy.
			JSONResponse:        true,
			MaxRequestBodyBytes: mcpMaxBodyBytes,
			// A client that hangs up stops the work it asked for.
			PropagateRequestCancellation: true,
			// The SDK refuses requests that arrive over a loopback connection
			// with a Host header naming another machine, to protect servers
			// on a developer's computer from DNS rebinding. Behind a proxy on
			// the same host every request looks like that and would be
			// refused. The attack needs a browser page, and a page is stopped
			// by the origin check in front of this handler, which does not
			// depend on how the request reached the process; nor has a page
			// any credentials to lend, since only a bearer token is accepted.
			DisableLocalhostProtection: true,
		},
	)
	return transport, scopes
}

// postOnly answers every method but POST with 405, as the Streamable HTTP
// transport asks of a server that offers no stream to GET.
func postOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeMCPError(w, http.StatusMethodNotAllowed, "method_not_allowed", "This endpoint accepts POST only.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// refuseReservedAccount keeps the demo account's address from being used
// through MCP, as getUser keeps it from the REST API. resolveUser refuses it
// again for each tool; refusing here answers with a status instead of a tool
// error, before anything is read. It is a plain 403 without a challenge:
// signing in again as the same account would change nothing.
func refuseReservedAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, _ := middleware.MCPIdentityFrom(auth.TokenInfoFromContext(r.Context()))
		if isReservedEmail(identity.Email) {
			writeMCPError(w, http.StatusForbidden, "access_denied", errReservedAccount.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireToolScopes answers a call to a tool with 403 insufficient_scope
// when the token was not granted the scope that tool needs. scopes maps a
// tool's name to that scope.
//
// The specification asks for this failure at the HTTP level, because the
// challenge on the 403 is what tells a client to send the user back to grant
// more. That means looking into the body before the transport does. The look
// decides nothing else: a body this function cannot read as a tool call is
// passed on untouched, and each tool checks its own scope again when it
// runs (see addMCPTool), so a request that hid its tool name from this
// parser but not from the SDK's still ends without the write happening.
func requireToolScopes(guard *middleware.MCPAuth, scopes map[string]string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcpMaxBodyBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeMCPError(w, http.StatusRequestEntityTooLarge, "request_too_large", "The request body is too large.")
				return
			}
			writeMCPError(w, http.StatusBadRequest, "invalid_request", "The request body could not be read.")
			return
		}

		token := auth.TokenInfoFromContext(r.Context())
		for _, tool := range toolsCalled(body) {
			if scope, known := scopes[tool]; known && !middleware.HasScope(token, scope) {
				guard.DenyScope(w, fmt.Sprintf("The %s tool needs the %s scope.", tool, scope))
				return
			}
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

// toolsCalled returns the names of the tools a JSON-RPC body asks to run. The
// body is one message or, from clients on older protocol versions, a batch.
func toolsCalled(body []byte) []string {
	messages := []json.RawMessage{body}
	if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(body, &messages); err != nil {
			return nil
		}
	}
	var names []string
	for _, message := range messages {
		// Members are looked up by their exact names, as the SDK reads them.
		// Decoding into a struct would match "Name" as well as "name", and a
		// call carrying both could show this function one tool and the SDK
		// another.
		var call, params map[string]json.RawMessage
		var method, name string
		if json.Unmarshal(message, &call) != nil ||
			json.Unmarshal(call["method"], &method) != nil || method != "tools/call" ||
			json.Unmarshal(call["params"], &params) != nil ||
			json.Unmarshal(params["name"], &name) != nil {
			continue
		}
		names = append(names, name)
	}
	return names
}

// writeMCPError answers a request the endpoint refused before it reached the
// protocol, in the same shape as the token errors.
func writeMCPError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}
