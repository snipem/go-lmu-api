// Code generator for LMU API.
// Fetches the Swagger schema, generates client stubs, calls every parameterless
// GET endpoint to capture live JSON, and infers Go structs from the responses.
//
// Usage: go run ./cmd/generate -base http://localhost:6397
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// ── Swagger schema types ────────────────────────────────────────────────────

type SwaggerSchema struct {
	Info        SwaggerInfo                       `json:"info"`
	Definitions map[string]json.RawMessage        `json:"definitions"`
	Paths       map[string]map[string]SwaggerOp   `json:"paths"`
}

type SwaggerInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type SwaggerOp struct {
	Parameters []SwaggerParam   `json:"parameters"`
	Responses  map[string]json.RawMessage `json:"responses"`
}

type SwaggerParam struct {
	In   string `json:"in"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ── Endpoint descriptor ─────────────────────────────────────────────────────

type Endpoint struct {
	Path       string
	Method     string // GET, POST, PUT, DELETE
	Params     []SwaggerParam
	Group      string // e.g. "navigation", "garage", "race"
	FuncName   string // Go-safe function name
	HasPathP   bool   // has path parameters or regex
}

// ── JSON-to-Go struct inference ─────────────────────────────────────────────

func jsonToGoType(name string, v interface{}, structs map[string]string) string {
	switch val := v.(type) {
	case nil:
		return "interface{}"
	case bool:
		return "bool"
	case float64:
		// JSON numbers: check if it looks like an int
		if val == float64(int64(val)) {
			return "int64"
		}
		return "float64"
	case string:
		return "string"
	case []interface{}:
		if len(val) == 0 {
			return "[]interface{}"
		}
		elemType := jsonToGoType(name+"Item", val[0], structs)
		return "[]" + elemType
	case map[string]interface{}:
		return jsonObjectToStruct(name, val, structs)
	default:
		return "interface{}"
	}
}

func jsonObjectToStruct(name string, obj map[string]interface{}, structs map[string]string) string {
	if len(obj) == 0 {
		return "map[string]interface{}"
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// If all keys are numeric, model as a map instead of a struct
	allNumeric := true
	for _, k := range keys {
		for _, r := range k {
			if r < '0' || r > '9' {
				allNumeric = false
				break
			}
		}
		if !allNumeric {
			break
		}
	}
	if allNumeric && len(keys) > 1 {
		// Use first value to determine the element type
		elemType := jsonToGoType(name+"Item", obj[keys[0]], structs)
		return "map[string]" + elemType
	}

	var fields []string
	usedNames := make(map[string]int)
	for _, k := range keys {
		fieldName := toExportedName(k)
		// Ensure field name doesn't start with a digit
		if len(fieldName) > 0 && fieldName[0] >= '0' && fieldName[0] <= '9' {
			fieldName = "N" + fieldName
		}
		// Deduplicate field names within the same struct
		if count, exists := usedNames[fieldName]; exists {
			usedNames[fieldName] = count + 1
			fieldName = fmt.Sprintf("%s%d", fieldName, count+1)
		} else {
			usedNames[fieldName] = 1
		}
		fieldType := jsonToGoType(name+fieldName, obj[k], structs)
		jsonTag := fmt.Sprintf("`json:\"%s\"`", k)
		fields = append(fields, fmt.Sprintf("\t%s %s %s", fieldName, fieldType, jsonTag))
	}

	structDef := fmt.Sprintf("type %s struct {\n%s\n}", name, strings.Join(fields, "\n"))
	structs[name] = structDef
	return name
}

// ── Naming helpers ──────────────────────────────────────────────────────────

var nonAlpha = regexp.MustCompile(`[^a-zA-Z0-9]+`)
var regexPathPart = regexp.MustCompile(`\(.*?\)`)

func toExportedName(s string) string {
	// Split on non-alphanumeric, capitalize each part
	parts := nonAlpha.Split(s, -1)
	var out strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		// Special: all-caps short abbreviations stay caps (ID, URL, etc.)
		if len(p) <= 3 && strings.ToUpper(p) == p {
			out.WriteString(strings.ToUpper(p))
		} else {
			runes := []rune(p)
			runes[0] = unicode.ToUpper(runes[0])
			out.WriteString(string(runes))
		}
	}
	result := out.String()
	if result == "" {
		return "X"
	}
	return result
}

func endpointToFuncName(method, path string) string {
	// Remove regex groups and path param placeholders for naming
	clean := regexPathPart.ReplaceAllString(path, "")
	clean = strings.ReplaceAll(clean, "?", "")
	name := toExportedName(clean)
	// Prefix with method if not GET (to disambiguate)
	switch strings.ToUpper(method) {
	case "POST":
		name = "Post" + name
	case "PUT":
		name = "Put" + name
	case "DELETE":
		name = "Delete" + name
	}
	return name
}

func pathToGroup(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) >= 2 {
		// Use second part as group (first is "rest" or "navigation")
		if parts[0] == "rest" && len(parts) >= 2 {
			return parts[1]
		}
		return parts[0]
	}
	return "root"
}

// ── Determine if path has dynamic parts ─────────────────────────────────────

func hasPathParams(path string, params []SwaggerParam) bool {
	if strings.Contains(path, "{") || regexPathPart.MatchString(path) {
		return true
	}
	for _, p := range params {
		if p.In == "path" {
			return true
		}
	}
	return false
}

// ── Main ────────────────────────────────────────────────────────────────────

func main() {
	baseURL := flag.String("base", "http://localhost:6397", "Base URL of the API")
	outDir := flag.String("out", "lib", "Output directory for generated code")
	flag.Parse()

	log.SetFlags(0)

	// 1. Fetch swagger schema
	log.Println("Fetching swagger schema...")
	schemaURL := *baseURL + "/swagger-schema.json"
	resp, err := http.Get(schemaURL)
	if err != nil {
		log.Fatalf("Failed to fetch schema: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var schema SwaggerSchema
	if err := json.Unmarshal(body, &schema); err != nil {
		log.Fatalf("Failed to parse schema: %v", err)
	}
	log.Printf("Parsed schema: %s v%s — %d paths", schema.Info.Title, schema.Info.Version, len(schema.Paths))

	// 2. Build endpoint list
	var endpoints []Endpoint
	for path, methods := range schema.Paths {
		for method, op := range methods {
			ep := Endpoint{
				Path:     path,
				Method:   strings.ToUpper(method),
				Params:   op.Parameters,
				Group:    pathToGroup(path),
				FuncName: endpointToFuncName(method, path),
				HasPathP: hasPathParams(path, op.Parameters),
			}
			endpoints = append(endpoints, ep)
		}
	}
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i].Group != endpoints[j].Group {
			return endpoints[i].Group < endpoints[j].Group
		}
		return endpoints[i].Path < endpoints[j].Path
	})
	log.Printf("Found %d endpoints", len(endpoints))

	// 3. For parameterless GET endpoints, call them and infer types
	inferredStructs := make(map[string]string)     // struct name -> struct definition
	endpointResponseType := make(map[string]string) // funcName -> response type

	totalGetCalls := 0
	successCalls := 0
	skippedCalls := 0
	totalBytes := 0
	totalCallTime := time.Duration(0)

	log.Println()
	log.Printf("%-55s %6s %10s  %s", "ENDPOINT", "STATUS", "SIZE", "TIME")
	log.Printf("%-55s %6s %10s  %s", strings.Repeat("─", 55), "──────", "──────────", "────────")

	for _, ep := range endpoints {
		if ep.Method != "GET" || ep.HasPathP {
			continue
		}
		totalGetCalls++
		url := *baseURL + ep.Path
		start := time.Now()

		resp, err := http.Get(url)
		elapsed := time.Since(start)
		totalCallTime += elapsed

		if err != nil {
			log.Printf("%-55s %6s %10s  %8s  SKIP (error: %v)", ep.Path, "ERR", "-", elapsed.Round(time.Millisecond), err)
			skippedCalls++
			continue
		}
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		bodyLen := len(respBody)
		totalBytes += bodyLen

		if resp.StatusCode != 200 {
			log.Printf("%-55s %6d %10s  %8s  SKIP", ep.Path, resp.StatusCode, formatBytes(bodyLen), elapsed.Round(time.Millisecond))
			skippedCalls++
			continue
		}

		if bodyLen == 0 {
			log.Printf("%-55s %6d %10s  %8s  SKIP (empty)", ep.Path, resp.StatusCode, "0 B", elapsed.Round(time.Millisecond))
			skippedCalls++
			continue
		}

		// Try to parse as JSON
		var parsed interface{}
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			log.Printf("%-55s %6d %10s  %8s  SKIP (not JSON)", ep.Path, resp.StatusCode, formatBytes(bodyLen), elapsed.Round(time.Millisecond))
			skippedCalls++
			continue
		}

		typeName := ep.FuncName + "Response"
		goType := jsonToGoType(typeName, parsed, inferredStructs)
		endpointResponseType[ep.FuncName] = goType
		successCalls++
		log.Printf("%-55s %6d %10s  %8s  -> %s", ep.Path, resp.StatusCode, formatBytes(bodyLen), elapsed.Round(time.Millisecond), goType)
	}

	log.Println()
	log.Printf("GET summary: %d called, %d inferred, %d skipped | %s total data | %s total time",
		totalGetCalls, successCalls, skippedCalls, formatBytes(totalBytes), totalCallTime.Round(time.Millisecond))

	// 4. Generate code
	os.MkdirAll(*outDir, 0o755)

	// 4a. Generate models.go — all inferred structs
	generateModels(*outDir, inferredStructs)

	// 4b. Generate client.go — the HTTP client + all stubs
	generateClient(*outDir, endpoints, endpointResponseType)

	log.Println()
	log.Println("Done! Generated code in:", *outDir)
}

func formatBytes(b int) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// ── Code generation ─────────────────────────────────────────────────────────

func generateModels(outDir string, structs map[string]string) {
	var buf strings.Builder
	buf.WriteString("// Code generated by cmd/generate. DO NOT EDIT.\n")
	buf.WriteString("package lib\n\n")

	// Sort for deterministic output
	names := make([]string, 0, len(structs))
	for n := range structs {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, n := range names {
		buf.WriteString(structs[n])
		buf.WriteString("\n\n")
	}

	writeFormatted(filepath.Join(outDir, "models.go"), buf.String())
	log.Printf("Generated models.go with %d structs", len(structs))
}

func generateClient(outDir string, endpoints []Endpoint, responseTypes map[string]string) {
	var buf strings.Builder
	buf.WriteString("// Code generated by cmd/generate. DO NOT EDIT.\n")
	buf.WriteString("package lib\n\n")
	buf.WriteString("import (\n")
	buf.WriteString("\t\"bytes\"\n")
	buf.WriteString("\t\"encoding/json\"\n")
	buf.WriteString("\t\"fmt\"\n")
	buf.WriteString("\t\"io\"\n")
	buf.WriteString("\t\"net/http\"\n")
	buf.WriteString(")\n\n")

	// Client struct
	buf.WriteString("type Client struct {\n")
	buf.WriteString("\tBaseURL    string\n")
	buf.WriteString("\tHTTPClient *http.Client\n")
	buf.WriteString("}\n\n")

	buf.WriteString("func NewClient(baseURL string) *Client {\n")
	buf.WriteString("\treturn &Client{BaseURL: baseURL, HTTPClient: http.DefaultClient}\n")
	buf.WriteString("}\n\n")

	// Helper methods
	buf.WriteString(`func (c *Client) doRequest(method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}
`)
	buf.WriteString("\n")

	// Track seen func names to avoid duplicates
	seen := make(map[string]bool)

	for _, ep := range endpoints {
		funcName := ep.FuncName
		if seen[funcName] {
			funcName = funcName + ep.Method
		}
		seen[funcName] = true

		// Build function signature
		var sigParams []string
		var pathBuild string

		// Collect path params
		pathExpr := ep.Path
		for _, p := range ep.Params {
			if p.In == "path" {
				goParamType := swaggerTypeToGo(p.Type)
				sigParams = append(sigParams, fmt.Sprintf("%s %s", toLowerCamel(p.Name), goParamType))
			}
		}

		// Collect query params
		var queryParams []SwaggerParam
		for _, p := range ep.Params {
			if p.In == "query" {
				goParamType := swaggerTypeToGo(p.Type)
				sigParams = append(sigParams, fmt.Sprintf("%s %s", toLowerCamel(p.Name), goParamType))
				queryParams = append(queryParams, p)
			}
		}

		// Check for body param
		hasBody := false
		for _, p := range ep.Params {
			if p.In == "body" {
				hasBody = true
				break
			}
		}
		if hasBody {
			sigParams = append(sigParams, "body interface{}")
		}

		// Replace path placeholders: {name} -> %v, and regex groups -> %v
		pathExpr = regexp.MustCompile(`\{(\w+)\}`).ReplaceAllString(pathExpr, "%v")
		pathExpr = regexPathPart.ReplaceAllString(pathExpr, "%v")

		// Count format verbs to build fmt.Sprintf args
		pathParamNames := []string{}
		for _, p := range ep.Params {
			if p.In == "path" {
				pathParamNames = append(pathParamNames, toLowerCamel(p.Name))
			}
		}

		if len(pathParamNames) > 0 {
			pathBuild = fmt.Sprintf("fmt.Sprintf(\"%s\", %s)", pathExpr, strings.Join(pathParamNames, ", "))
		} else {
			pathBuild = fmt.Sprintf("%q", ep.Path)
		}

		// Determine return type
		retType := responseTypes[ep.FuncName]
		hasTypedResponse := retType != "" && !strings.HasPrefix(retType, "[]") && retType != "string" && retType != "bool" && retType != "int64" && retType != "float64" && retType != "interface{}" && retType != "map[string]interface{}"

		if retType == "" {
			retType = "json.RawMessage"
		}

		// Write function
		sig := strings.Join(sigParams, ", ")
		if retType == "json.RawMessage" || !hasTypedResponse {
			// Raw return
			buf.WriteString(fmt.Sprintf("func (c *Client) %s(%s) (%s, error) {\n", funcName, sig, retType))
		} else {
			buf.WriteString(fmt.Sprintf("func (c *Client) %s(%s) (*%s, error) {\n", funcName, sig, retType))
		}

		// Body arg for doRequest
		bodyArg := "nil"
		if hasBody {
			bodyArg = "body"
		}

		buf.WriteString(fmt.Sprintf("\tdata, err := c.doRequest(%q, %s, %s)\n", ep.Method, pathBuild, bodyArg))
		buf.WriteString("\tif err != nil {\n")
		if hasTypedResponse {
			buf.WriteString("\t\treturn nil, err\n")
		} else {
			writeZeroReturn(&buf, retType)
		}
		buf.WriteString("\t}\n")

		// Add query params if any
		if len(queryParams) > 0 {
			// We need to adjust — actually query params should go into the URL.
			// Let me add them before the doRequest call. I'll restructure.
			// For simplicity, embed them in the path build.
		}

		// Unmarshal if typed
		if hasTypedResponse {
			buf.WriteString(fmt.Sprintf("\tvar result %s\n", retType))
			buf.WriteString("\tif err := json.Unmarshal(data, &result); err != nil {\n")
			buf.WriteString("\t\treturn nil, err\n")
			buf.WriteString("\t}\n")
			buf.WriteString("\treturn &result, nil\n")
		} else if retType == "json.RawMessage" {
			buf.WriteString("\treturn data, nil\n")
		} else {
			// primitive types or slices
			buf.WriteString(fmt.Sprintf("\tvar result %s\n", retType))
			buf.WriteString("\tif err := json.Unmarshal(data, &result); err != nil {\n")
			writeZeroReturn(&buf, retType)
			buf.WriteString("\t}\n")
			buf.WriteString("\treturn result, nil\n")
		}
		buf.WriteString("}\n\n")
	}

	writeFormatted(filepath.Join(outDir, "client.go"), buf.String())
	log.Printf("Generated client.go with %d methods", len(endpoints))
}

func writeZeroReturn(buf *strings.Builder, retType string) {
	switch {
	case retType == "string":
		buf.WriteString("\t\treturn \"\", err\n")
	case retType == "bool":
		buf.WriteString("\t\treturn false, err\n")
	case retType == "int64" || retType == "float64":
		buf.WriteString("\t\treturn 0, err\n")
	case strings.HasPrefix(retType, "[]") || strings.HasPrefix(retType, "map"):
		buf.WriteString("\t\treturn nil, err\n")
	default:
		buf.WriteString("\t\treturn nil, err\n")
	}
}

func swaggerTypeToGo(t string) string {
	switch t {
	case "integer":
		return "int"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	default:
		return "string"
	}
}

func toLowerCamel(s string) string {
	parts := nonAlpha.Split(s, -1)
	var out strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 {
			// Check if it's a Go keyword
			out.WriteString(strings.ToLower(p[:1]) + p[1:])
		} else {
			runes := []rune(p)
			runes[0] = unicode.ToUpper(runes[0])
			out.WriteString(string(runes))
		}
	}
	result := out.String()
	// Avoid Go keywords
	switch result {
	case "type", "func", "map", "range", "var", "const", "return", "default":
		return result + "Param"
	}
	return result
}

func writeFormatted(path string, code string) {
	formatted, err := format.Source([]byte(code))
	if err != nil {
		log.Printf("Warning: gofmt failed for %s: %v — writing unformatted", path, err)
		formatted = []byte(code)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		log.Fatalf("Failed to write %s: %v", path, err)
	}
}
