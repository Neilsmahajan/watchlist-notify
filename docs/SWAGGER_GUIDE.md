# Swagger/OpenAPI Documentation Guide

Complete guide for working with the Watchlist Notify API documentation using Swagger/OpenAPI.

## Quick Start

### Viewing the Documentation

**Production:**

```
https://api.watchlistnotify.com/swagger/index.html
```

**Local Development:**

```
http://localhost:8080/swagger/index.html
```

### Downloading the Spec

**OpenAPI JSON:**

```
https://api.watchlistnotify.com/swagger/doc.json
```

**OpenAPI YAML:**
Available in the repository at `docs/swagger.yaml`

## Regenerating Documentation

### When to Regenerate

Regenerate the Swagger docs whenever you:

- Add new endpoints
- Modify handler signatures
- Change request/response structures
- Update API descriptions or tags

### Using Make

```bash
make swagger
```

### Manual Generation

```bash
swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal
```

### What Gets Generated

- `docs/docs.go` - Go code to embed the spec
- `docs/swagger.json` - OpenAPI 3.0 specification (JSON)
- `docs/swagger.yaml` - OpenAPI 3.0 specification (YAML)

**Note:** These files are auto-generated and git-ignored. Do not edit them manually.

## Using Swagger UI

### Authentication

1. Click the **"Authorize"** button at the top right
2. Enter your JWT token in the format:
   ```
   Bearer YOUR_JWT_TOKEN_HERE
   ```
3. Click **"Authorize"** then **"Close"**
4. All subsequent requests will include the token automatically

### Making Requests

1. Expand an endpoint by clicking on it
2. Click **"Try it out"**
3. Fill in required parameters
4. Click **"Execute"**
5. View the response below

### Features

- ✅ **Interactive Testing** - Try endpoints directly in the browser
- ✅ **Auto-completion** - See available parameters and schemas
- ✅ **Response Preview** - View actual responses with status codes
- ✅ **cURL Export** - Copy cURL commands for any request
- ✅ **Schema Validation** - Request validation against OpenAPI schema

## Adding Documentation to New Endpoints

### Basic Handler Annotation

```go
// handlerName godoc
// @Summary Short description (appears in endpoint list)
// @Description Longer description with details
// @Tags Category
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param paramName query string false "Parameter description"
// @Success 200 {object} models.ResponseType "Success description"
// @Failure 400 {object} ErrorResponse "Error description"
// @Router /api/path [get]
func (s *Server) handlerName(c *gin.Context) {
    // handler code
}
```

### Annotation Reference

#### Required Annotations

```go
// @Summary Brief description (max 120 chars)
// @Router /api/path [method]
```

#### Common Annotations

```go
// @Description Detailed explanation (supports markdown)
// @Tags GroupName                    // Swagger UI grouping
// @Security BearerAuth               // Requires authentication
// @Accept json                       // Request content type
// @Produce json                      // Response content type
```

#### Parameter Annotations

**Query Parameter:**

```go
// @Param name query type required "description" default(value) Enums(opt1, opt2)
```

**Path Parameter:**

```go
// @Param id path string true "User ID"
```

**Body Parameter:**

```go
// @Param body body models.RequestType true "Request description"
```

**Form Data:**

```go
// @Param file formData file true "File to upload"
```

#### Response Annotations

```go
// @Success 200 {object} models.User "Success description"
// @Success 201 {object} models.Created "Resource created"
// @Failure 400 {object} ErrorResponse "Bad request"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 404 {object} ErrorResponse "Not found"
// @Failure 500 {object} ErrorResponse "Internal error"
```

### Example: Complete Endpoint Documentation

```go
// getUserProfile godoc
// @Summary Get user profile
// @Description Returns the authenticated user's complete profile including preferences and subscriptions
// @Tags Users
// @Security BearerAuth
// @Produce json
// @Success 200 {object} models.User "User profile"
// @Failure 401 {object} ErrorResponse "Unauthorized - invalid or missing token"
// @Failure 500 {object} ErrorResponse "Internal server error"
// @Router /api/me [get]
func (s *Server) getUserProfile(c *gin.Context) {
    user, ok := s.getUser(c)
    if !ok {
        return
    }
    c.JSON(http.StatusOK, user)
}
```

### Example: Endpoint with Parameters

```go
// searchContent godoc
// @Summary Search movies and TV shows
// @Description Search for content via TMDb with filters and pagination
// @Tags Content Discovery
// @Security BearerAuth
// @Produce json
// @Param query query string true "Search query"
// @Param type query string false "Content type" default(movie) Enums(movie, tv)
// @Param page query int false "Page number" default(1) minimum(1) maximum(1000)
// @Param include_adult query boolean false "Include adult content" default(false)
// @Success 200 {object} map[string]interface{} "Search results with pagination"
// @Failure 400 {object} ErrorResponse "Invalid parameters"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 502 {object} ErrorResponse "Upstream API error"
// @Router /api/search [get]
func (s *Server) searchContent(c *gin.Context) {
    // handler implementation
}
```

### Example: POST Endpoint with Body

```go
// createItem godoc
// @Summary Add item to watchlist
// @Description Create a new watchlist entry for a movie or TV show
// @Tags Watchlist
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body object{title=string,type=string,year=int,tmdb_id=int} true "Watchlist item"
// @Success 201 {object} models.WatchlistItem "Created item"
// @Failure 400 {object} ErrorResponse "Invalid request body"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 409 {object} ErrorResponse "Duplicate item"
// @Router /api/watchlist [post]
func (s *Server) createItem(c *gin.Context) {
    // handler implementation
}
```

## Best Practices

### Documentation Quality

1. **Be Descriptive** - Write clear, concise summaries
2. **Include Examples** - Use `example:"value"` in struct tags
3. **Document Errors** - List all possible error responses
4. **Use Enums** - Specify valid values for parameters
5. **Add Defaults** - Show default values for optional params

### Struct Tags for Better Docs

```go
type CreateRequest struct {
    Title  string   `json:"title" binding:"required" example:"Inception"`
    Type   string   `json:"type" enums:"movie,tv" default:"movie"`
    Year   int      `json:"year" example:"2010"`
    Tags   []string `json:"tags,omitempty" example:"sci-fi,thriller"`
}
```

### Consistent Error Responses

Always use the `ErrorResponse` struct for errors:

```go
type ErrorResponse struct {
    Error string `json:"error" example:"invalid request"`
}
```

### Security Annotations

For protected endpoints, always include:

```go
// @Security BearerAuth
```

For public endpoints, omit the security annotation.

## Troubleshooting

### Swag Command Not Found

```bash
go install github.com/swaggo/swag/cmd/swag@latest
```

Make sure `$GOPATH/bin` is in your `PATH`.

### Build Errors After Generating Docs

If you get import errors for `docs` package:

1. Ensure you've run `swag init` successfully
2. Check that `docs/docs.go` exists
3. Try `go mod tidy`

### Annotations Not Appearing

Common issues:

- Missing blank comment line before `godoc` comment
- Incorrect annotation syntax
- Handler not registered in router
- Forgot to regenerate with `make swagger`

### Wrong Schema Generated

If models aren't rendering correctly:

- Ensure struct fields are exported (capitalized)
- Add JSON tags to all fields
- Use `--parseInternal` flag when generating
- Check for import cycles

## Advanced Features

### Custom Types in Responses

```go
// @Success 200 {object} map[string]interface{} "Custom response"
// @Success 200 {array} models.Item "List of items"
// @Success 200 {string} string "Plain text response"
```

### Multiple Success Responses

```go
// @Success 200 {object} models.User "User found"
// @Success 201 {object} models.User "User created"
```

### Header Parameters

```go
// @Param Authorization header string true "Bearer token"
```

### File Uploads

```go
// @Accept multipart/form-data
// @Param file formData file true "CSV file to import"
```

### Markdown in Descriptions

```go
// @Description This endpoint supports:
// @Description - Feature 1
// @Description - Feature 2
// @Description
// @Description **Important:** Always validate input
```

## CI/CD Integration

### Pre-commit Hook

Create `.git/hooks/pre-commit`:

```bash
#!/bin/bash
make swagger
git add docs/
```

### GitHub Actions

```yaml
- name: Generate Swagger docs
  run: |
    go install github.com/swaggo/swag/cmd/swag@latest
    make swagger
```

## Additional Resources

- **Swaggo Documentation**: https://github.com/swaggo/swag
- **OpenAPI Spec**: https://swagger.io/specification/
- **Swagger UI**: https://swagger.io/tools/swagger-ui/
- **API Documentation**: See `docs/API.md` for complete API reference

## Common Patterns

### Pagination Endpoint

```go
// @Param limit query int false "Items per page" default(20) maximum(1000)
// @Param offset query int false "Items to skip" default(0)
```

### Filter Parameters

```go
// @Param status query string false "Filter by status" Enums(planned, watching, finished)
// @Param type query string false "Filter by type" Enums(movie, show)
```

### Search Endpoint

```go
// @Param query query string true "Search query" minlength(1)
// @Param page query int false "Page number" default(1) minimum(1)
```

### Batch Operations

```go
// @Param body body object{items=[]object{id=int,type=string}} true "Batch request"
```

## Migration Notes

If you're migrating from older documentation:

1. **Run Initial Generation:**

   ```bash
   make swagger
   ```

2. **Review Generated Spec:**
   Check `docs/swagger.json` for accuracy

3. **Update Annotations:**
   Add missing `@Summary` and `@Router` annotations

4. **Test in Swagger UI:**
   Verify all endpoints appear correctly

5. **Update Bruno Collection:**
   Ensure Bruno requests match OpenAPI spec

## Support

For issues or questions:

- Check Swaggo GitHub Issues: https://github.com/swaggo/swag/issues
- Review OpenAPI specification
- Contact: contact@watchlistnotify.com
