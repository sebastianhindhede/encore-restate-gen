package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Global variables
var (
	globalPackageManager     string
	restatedModulesInstalled bool
	restatedDepsMutex        sync.Mutex
	projectRoot              string

	// Store generated TemplateData per service directory.
	generatedDataMap      = make(map[string]TemplateData)
	generatedDataMapMutex sync.Mutex
)

//go:embed assets_dist/*
var assets embed.FS

// detectPackageManager checks for popular lock files in the given directory and returns
// "yarn", "pnpm", or defaults to "npm".
func detectPackageManager(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		return "yarn"
	}
	if _, err := os.Stat(filepath.Join(dir, "pnpm-lock.yaml")); err == nil {
		return "pnpm"
	}
	// Default to npm.
	return "npm"
}

// checkRestateModules reads the project's package.json (in dir) and returns true if all
// three required ReState packages are present (either in dependencies or devDependencies).
func checkRestateModules(dir string) (bool, error) {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := ioutil.ReadFile(pkgPath)
	if err != nil {
		return false, err
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false, err
	}
	required := []string{
		"@restatedev/restate-sdk",
		"@restatedev/restate-sdk-clients",
		"@restatedev/restate-sdk-core",
	}
	for _, dep := range required {
		if _, ok := pkg.Dependencies[dep]; !ok {
			if _, ok2 := pkg.DevDependencies[dep]; !ok2 {
				return false, nil
			}
		}
	}
	return true, nil
}

// installRestateModules installs any missing ReState modules using the detected package manager.
func installRestateModules(dir string) error {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := ioutil.ReadFile(pkgPath)
	if err != nil {
		return err
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return err
	}
	required := []string{
		"@restatedev/restate-sdk",
		"@restatedev/restate-sdk-clients",
		"@restatedev/restate-sdk-core",
	}
	missing := []string{}
	for _, dep := range required {
		if _, ok := pkg.Dependencies[dep]; !ok {
			if _, ok2 := pkg.DevDependencies[dep]; !ok2 {
				missing = append(missing, dep)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}

	var cmd *exec.Cmd
	switch globalPackageManager {
	case "yarn":
		args := append([]string{"add"}, missing...)
		cmd = exec.Command("yarn", args...)
	case "pnpm":
		args := append([]string{"add"}, missing...)
		cmd = exec.Command("pnpm", args...)
	case "npm":
		args := append([]string{"install"}, missing...)
		cmd = exec.Command("npm", args...)
	default:
		return fmt.Errorf("unsupported package manager: %s", globalPackageManager)
	}
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Printf("Installing missing dependencies: %v", missing)
	return cmd.Run()
}

// ensureRestateModulesInstalled checks if the required modules are installed in dir.
func ensureRestateModulesInstalled(dir string) error {
	restatedDepsMutex.Lock()
	defer restatedDepsMutex.Unlock()

	if restatedModulesInstalled {
		return nil
	}
	installed, err := checkRestateModules(dir)
	if err != nil {
		return err
	}
	if installed {
		restatedModulesInstalled = true
		return nil
	}
	log.Printf("Required ReState modules are not installed. Installing using %s...", globalPackageManager)
	if err := installRestateModules(dir); err != nil {
		return err
	}
	// Re-check after installation.
	installed, err = checkRestateModules(dir)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("failed to install required ReState modules")
	}
	restatedModulesInstalled = true
	log.Printf("ReState modules installed successfully.")
	return nil
}

// updateTsConfig updates the tsconfig.json file.
func updateTsConfig(root string) error {
	tsconfigPath := filepath.Join(root, "tsconfig.json")
	data, err := ioutil.ReadFile(tsconfigPath)
	if err != nil {
		return err
	}
	content := string(data)

	// If the file already contains the required entries, do nothing.
	if strings.Contains(content, "\"~restate\"") &&
		strings.Contains(content, "\"~restate/*\"") &&
		strings.Contains(content, "\"**/*.ts\"") &&
		strings.Contains(content, "\"./**/*.ts\"") &&
		strings.Contains(content, "\"./restate.gen/**/*.ts\"") {
		return nil
	}

	// Patch the "compilerOptions.paths" block.
	pathsRe := regexp.MustCompile(`("paths"\s*:\s*\{)([\s\S]*?)(\s*\})`)
	content = pathsRe.ReplaceAllStringFunc(content, func(match string) string {
		submatches := pathsRe.FindStringSubmatch(match)
		if len(submatches) < 4 {
			return match
		}
		prefix := submatches[1]
		body := submatches[2]
		suffix := submatches[3]
		if !strings.Contains(body, "\"~restate\"") {
			body = strings.TrimRight(body, " \n\r\t")
			body = strings.TrimRight(body, ",")
			if body != "" {
				body += ","
			}
			body += "\n      \"~restate\": [\"./restate.gen/index.ts\"],\n      \"~restate/*\": [\"./restate.gen/*\"]"
		}
		body = strings.TrimRight(body, "\n")
		return prefix + body + suffix
	})

	// Patch the "include" array.
	includeRe := regexp.MustCompile(`("include"\s*:\s*\[)([\s\S]*?)(\s*\])`)
	if includeRe.MatchString(content) {
		content = includeRe.ReplaceAllStringFunc(content, func(match string) string {
			submatches := includeRe.FindStringSubmatch(match)
			if len(submatches) < 4 {
				return match
			}
			prefix := submatches[1]
			body := strings.TrimSpace(submatches[2])
			suffix := submatches[3]
			var elements []string
			if body != "" {
				for _, elem := range strings.Split(body, ",") {
					elem = strings.TrimSpace(elem)
					if elem != "" {
						elements = append(elements, elem)
					}
				}
			}
			required := []string{`"**/*.ts"`, `"./**/*.ts"`, `"./restate.gen/**/*.ts"`}
			for _, req := range required {
				found := false
				for _, elem := range elements {
					if elem == req {
						found = true
						break
					}
				}
				if !found {
					elements = append(elements, req)
				}
			}
			newBody := "\n    " + strings.Join(elements, ",\n    ") + "\n"
			return prefix + newBody + suffix
		})
	} else {
		content = strings.TrimRight(content, " \n\r\t")
		if strings.HasSuffix(content, "}") {
			content = content[:len(content)-1] + ",\n  \"include\": [\n    \"**/*.ts\",\n    \"./**/*.ts\",\n    \"./restate.gen/**/*.ts\"\n  ]\n}"
		}
	}

	content = strings.ReplaceAll(content, "}\n,", "},")
	return ioutil.WriteFile(tsconfigPath, []byte(content), 0644)
}

// HandlerEntry holds information about an exported handler.
type HandlerEntry struct {
	ExportName string `json:"exportName"` // e.g. "greetHandler"
	Source     string `json:"source"`     // e.g. "./greeter"
	Type       string `json:"type"`       // "service", "workflow", or "virtualObject"
}

// Manifest is the output of the Node parser.
type Manifest struct {
	ServiceName string         `json:"serviceName"`
	Handlers    []HandlerEntry `json:"handlers"`
}

// GroupedHandler groups handler entries by their Source.
type GroupedHandler struct {
	Source   string
	Handlers []HandlerEntry
}

// TemplateData holds data passed to our combined generated template.
// FilePath is stored for later use in generating central exports.
type TemplateData struct {
	ServiceName        string
	ServiceNameTrimmed string
	ServiceGroup       []GroupedHandler
	WorkflowGroup      []GroupedHandler
	VirtualObjectGroup []GroupedHandler
	FilePath           string
}

// Combined generated template.
const combinedTemplate = `// This file is automatically generated by encore-restate-gen.
// Do not edit this file directly.

{{ if .ServiceGroup -}}
{{- range .ServiceGroup }}
import { {{- range $i, $h := .Handlers }}{{if $i}}, {{end}}{{ $h.ExportName }} as __{{ $h.ExportName }}{{ end }} } from "{{ .Source }}";
{{- end }}
{{ end }}

{{ if .WorkflowGroup -}}
{{- range .WorkflowGroup }}
import { {{- range $i, $h := .Handlers }}{{if $i}}, {{end}}{{ $h.ExportName }} as __{{ $h.ExportName }}{{ end }} } from "{{ .Source }}";
{{- end }}
{{ end }}

{{ if .VirtualObjectGroup -}}
{{- range .VirtualObjectGroup }}
import { {{- range $i, $h := .Handlers }}{{if $i}}, {{end}}{{ $h.ExportName }} as __{{ $h.ExportName }}{{ end }} } from "{{ .Source }}";
{{- end }}
{{ end }}

import { api } from "encore.dev/api";
import { endpoint } from "@restatedev/restate-sdk/fetch";
import * as restate from "@restatedev/restate-sdk";
import { buildEncoreRestateHandler } from "~restate";

// Build objects for each category.
{{ if .ServiceGroup -}}
export const _{{.ServiceNameTrimmed}}Service = restate.service({
  name: '{{.ServiceNameTrimmed}}Service',
  handlers: {
    {{- range .ServiceGroup }}
      {{- range .Handlers }}
        {{ .ExportName }}: __{{ .ExportName }},
      {{- end }}
    {{- end }}
  },
});
{{ end }}

{{ if .WorkflowGroup -}}
export const _{{.ServiceNameTrimmed}}Workflow = restate.workflow({
  name: '{{.ServiceNameTrimmed}}Workflow',
  handlers: {
    {{- range .WorkflowGroup }}
      {{- range .Handlers }}
        {{ .ExportName }}: __{{ .ExportName }},
      {{- end }}
    {{- end }}
  },
});
{{ end }}

{{ if .VirtualObjectGroup -}}
export const _{{.ServiceNameTrimmed}}Object = restate.object({
  name: '{{.ServiceNameTrimmed}}Object',
  handlers: {
    {{- range .VirtualObjectGroup }}
      {{- range .Handlers }}
        {{ .ExportName }}: __{{ .ExportName }},
      {{- end }}
    {{- end }}
  },
});
{{ end }}

// Bind all defined objects to the same endpoint.
const restateEndpoint = endpoint();
{{ if .ServiceGroup }} restateEndpoint.bind(_{{.ServiceNameTrimmed}}Service); {{ end }}
{{ if .WorkflowGroup }} restateEndpoint.bind(_{{.ServiceNameTrimmed}}Workflow); {{ end }}
{{ if .VirtualObjectGroup }} restateEndpoint.bind(_{{.ServiceNameTrimmed}}Object); {{ end }}

// Build common endpoint handler.
export const handler = buildEncoreRestateHandler(restateEndpoint.handler().fetch);

{{- range .ServiceGroup }}
  {{- range .Handlers }}
export const {{.ExportName}} = api.raw(
  { expose: false, path: '/{{$.ServiceName}}/invoke/{{$.ServiceNameTrimmed}}Service/{{.ExportName}}', method: "POST" },
  handler,
);
  {{- end }}
{{- end }}

{{- range .WorkflowGroup }}
  {{- range .Handlers }}
export const {{.ExportName}} = api.raw(
  { expose: false, path: '/{{$.ServiceName}}/invoke/{{$.ServiceNameTrimmed}}Workflow/{{.ExportName}}', method: "POST" },
  handler,
);
  {{- end }}
{{- end }}

{{- range .VirtualObjectGroup }}
  {{- range .Handlers }}
export const {{.ExportName}} = api.raw(
  { expose: false, path: '/{{$.ServiceName}}/invoke/{{$.ServiceNameTrimmed}}Object/{{.ExportName}}', method: "POST" },
  handler,
);
  {{- end }}
{{- end }}

export const discover = api.raw(
  { expose: false, path: '/{{.ServiceName}}/discover', method: "GET" },
  handler,
);

{{ if .ServiceGroup }}
export const {{.ServiceNameTrimmed}}Service: typeof _{{.ServiceNameTrimmed}}Service = {
  name: "{{.ServiceNameTrimmed}}Service",
};
{{ end }}
{{ if .WorkflowGroup }}
export const {{.ServiceNameTrimmed}}Workflow: typeof _{{.ServiceNameTrimmed}}Workflow = {
  name: "{{.ServiceNameTrimmed}}Workflow",
};
{{ end }}
{{ if .VirtualObjectGroup }}
export const {{.ServiceNameTrimmed}}Object: typeof _{{.ServiceNameTrimmed}}Object = {
  name: "{{.ServiceNameTrimmed}}Object",
};
{{ end }}
`

// extractAssets extracts the embedded assets to a temporary directory.
func extractAssets() (string, error) {
	tempDir, err := ioutil.TempDir("", "assets_dist")
	if err != nil {
		return "", err
	}
	err = fs.WalkDir(assets, "assets_dist", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel("assets_dist", path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(tempDir, relPath)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}
		return ioutil.WriteFile(targetPath, data, 0755)
	})
	if err != nil {
		return "", err
	}
	return tempDir, nil
}

// runNodeScript runs the Node extraction script and returns the manifest.
func runNodeScript(dir string) (*Manifest, error) {
	assetsDir, err := extractAssets()
	if err != nil {
		return nil, fmt.Errorf("failed to extract embedded assets: %v", err)
	}
	defer os.RemoveAll(assetsDir)
	scriptPath := filepath.Join(assetsDir, "index.js")
	cmd := exec.Command("node", scriptPath, dir)
	cmd.Dir = assetsDir
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run Node script: %v, output: %s", err, string(outBytes))
	}
	var manifest Manifest
	if err := json.Unmarshal(outBytes, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse JSON manifest: %v, output: %s", err, string(outBytes))
	}
	return &manifest, nil
}

// groupHandlers groups a slice of HandlerEntry by Source.
func groupHandlers(handlers []HandlerEntry) []GroupedHandler {
	groupMap := make(map[string][]HandlerEntry)
	for _, h := range handlers {
		groupMap[h.Source] = append(groupMap[h.Source], h)
	}
	var groups []GroupedHandler
	for src, hs := range groupMap {
		groups = append(groups, GroupedHandler{Source: src, Handlers: hs})
	}
	return groups
}

func trimSuffixes(s string) string {
	suffixes := []string{"Workflow", "Object", "Service"}
	for _, suf := range suffixes {
		s = strings.TrimSuffix(s, suf)
	}
	return s
}

// generateFile generates the combined file using the template.
func generateFile(filePath string, data TemplateData) error {
	tmpl, err := template.New("generated").Parse(combinedTemplate)
	if err != nil {
		return err
	}
	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	return tmpl.Execute(f, data)
}

// processDirectory processes a service directory (one containing an encore.service.ts file),
// runs the Node script to extract handlers, groups them, and generates the unified <servicename>.restate.ts file.
func processDirectory(serviceDir string) {
	// Before code generation, ensure required ReState modules are installed.
	if err := ensureRestateModulesInstalled(projectRoot); err != nil {
		log.Printf("Error ensuring ReState modules installed in %s: %v", serviceDir, err)
		return
	}

	manifest, err := runNodeScript(serviceDir)
	if err != nil {
		log.Printf("Error extracting manifest from %s: %v", serviceDir, err)
		return
	}
	if manifest.ServiceName == "" {
		return
	}
	genFileName := fmt.Sprintf("%s.restate.ts", strings.ToLower(manifest.ServiceName))
	generatedFilePath := filepath.Join(serviceDir, genFileName)

	// Filter handlers by category.
	serviceHandlers := []HandlerEntry{}
	workflowHandlers := []HandlerEntry{}
	virtualObjectHandlers := []HandlerEntry{}
	for _, h := range manifest.Handlers {
		switch h.Type {
		case "service":
			serviceHandlers = append(serviceHandlers, h)
		case "workflow":
			workflowHandlers = append(workflowHandlers, h)
		case "virtualObject":
			virtualObjectHandlers = append(virtualObjectHandlers, h)
		}
	}

	// If no handlers are found, delete any existing generated file and remove stored data.
	if len(serviceHandlers)+len(workflowHandlers)+len(virtualObjectHandlers) == 0 {
		if _, err := os.Stat(generatedFilePath); err == nil {
			os.Remove(generatedFilePath)
			log.Printf("Removed generated file: %s", generatedFilePath)
		}
		generatedDataMapMutex.Lock()
		delete(generatedDataMap, serviceDir)
		generatedDataMapMutex.Unlock()
		return
	}

	// Build TemplateData.
	data := TemplateData{
		ServiceName:        manifest.ServiceName,
		ServiceNameTrimmed: trimSuffixes(manifest.ServiceName),
		ServiceGroup:       groupHandlers(serviceHandlers),
		WorkflowGroup:      groupHandlers(workflowHandlers),
		VirtualObjectGroup: groupHandlers(virtualObjectHandlers),
		FilePath:           generatedFilePath,
	}

	if err := generateFile(generatedFilePath, data); err != nil {
		log.Printf("Error generating file %s: %v", generatedFilePath, err)
	} else {
		log.Printf("Generated file: %s", generatedFilePath)
	}

	// Store the generated data for later use in central index generation.
	generatedDataMapMutex.Lock()
	generatedDataMap[serviceDir] = data
	generatedDataMapMutex.Unlock()
}

// generateCentralIndex generates the central index files using the stored TemplateData.
func generateCentralIndex(root string) error {
	centralDirs := map[string]string{
		"service":       filepath.Join(root, "restate.gen", "services"),
		"workflow":      filepath.Join(root, "restate.gen", "workflows"),
		"virtualobject": filepath.Join(root, "restate.gen", "objects"),
	}
	// Create each central directory.
	for _, dir := range centralDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create central index directory: %v", err)
		}
	}

	// Clean up stored data for files that no longer exist.
	generatedDataMapMutex.Lock()
	for key, data := range generatedDataMap {
		if _, err := os.Stat(data.FilePath); os.IsNotExist(err) {
			delete(generatedDataMap, key)
		}
	}
	generatedDataMapMutex.Unlock()

	exports := map[string][]string{
		"service":       {},
		"workflow":      {},
		"virtualobject": {},
	}

	// Iterate over stored TemplateData.
	generatedDataMapMutex.Lock()
	for _, data := range generatedDataMap {
		if len(data.ServiceGroup) > 0 {
			rel, err := filepath.Rel(centralDirs["service"], data.FilePath)
			if err == nil {
				rel = strings.ReplaceAll(filepath.ToSlash(rel), ".ts", "")
				line := fmt.Sprintf("export { %sService as %s } from './%s';", data.ServiceNameTrimmed, data.ServiceNameTrimmed, rel)
				exports["service"] = append(exports["service"], line)
			}
		}
		if len(data.WorkflowGroup) > 0 {
			rel, err := filepath.Rel(centralDirs["workflow"], data.FilePath)
			if err == nil {
				rel = strings.ReplaceAll(filepath.ToSlash(rel), ".ts", "")
				line := fmt.Sprintf("export { %sWorkflow as %s } from './%s';", data.ServiceNameTrimmed, data.ServiceNameTrimmed, rel)
				exports["workflow"] = append(exports["workflow"], line)
			}
		}
		if len(data.VirtualObjectGroup) > 0 {
			rel, err := filepath.Rel(centralDirs["virtualobject"], data.FilePath)
			if err == nil {
				rel = strings.ReplaceAll(filepath.ToSlash(rel), ".ts", "")
				line := fmt.Sprintf("export { %sObject as %s } from './%s';", data.ServiceNameTrimmed, data.ServiceNameTrimmed, rel)
				exports["virtualobject"] = append(exports["virtualobject"], line)
			}
		}
	}
	generatedDataMapMutex.Unlock()

	// If no exports exist in a category, add a default export.
	for cat, lines := range exports {
		if len(lines) == 0 {
			exports[cat] = []string{"export default {};"}
		}
	}

	// Write central index files.
	for cat, dir := range centralDirs {
		indexContent := strings.Join(exports[cat], "\n")
		indexPath := filepath.Join(dir, "index.ts")
		if err := ioutil.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
			return fmt.Errorf("error writing index for %s: %v", cat, err)
		}
	}

	// Generate root index file.
	restDir := filepath.Join(root, "restate.gen")
	if err := os.MkdirAll(restDir, 0755); err != nil {
		return fmt.Errorf("failed to create restate.gen directory: %v", err)
	}
	rootIndexContent := `// This file is automatically generated by encore-restate-gen.
// Do not edit this file directly.

import { api as _api } from "encore.dev/api";
import type { IncomingMessage, ServerResponse } from "node:http";
import * as clients from "@restatedev/restate-sdk-clients";
import type {
  Service,
  VirtualObject,
  ServiceDefinitionFrom,
  VirtualObjectDefinitionFrom,
  WorkflowDefinitionFrom,
  Workflow,
} from "@restatedev/restate-sdk-core";
export * as services from "~restate/services";
export * as workflows from "~restate/workflows";
export * as objects from "~restate/objects";

let cachedClient: ReturnType<typeof clients.connect> | undefined;
export const getClient = () => {
  if (!cachedClient) {
    cachedClient = clients.connect({ url: process.env.RESTATE_SERVER_URL ?? "http://localhost:8080" });
  }
  return cachedClient;
};

export const serviceClient = <D>(svc: ServiceDefinitionFrom<D>): clients.IngressClient<Service<D>> =>
  getClient().serviceClient(svc);

export const objectClient = <D>(obj: VirtualObjectDefinitionFrom<D>, key: string): clients.IngressClient<VirtualObject<D>> =>
  getClient().objectClient(obj, key);

export const serviceSendClient = <D>(svc: ServiceDefinitionFrom<D>): clients.IngressSendClient<Service<D>> =>
  getClient().serviceSendClient(svc);

export const objectSendClient = <D>(obj: VirtualObjectDefinitionFrom<D>, key: string): clients.IngressSendClient<VirtualObject<D>> =>
  getClient().objectSendClient(obj, key);

export const workflowClient = <D>(wf: WorkflowDefinitionFrom<D>, key: string): clients.IngressWorkflowClient<Workflow<D>> =>
  getClient().workflowClient(wf, key);

export function buildEncoreRestateHandler(fetch: (request: Request, ...extraArgs: unknown[]) => Promise<Response>) {
  return (req: IncomingMessage, resp: ServerResponse<IncomingMessage>) => {
    getBody(req)
      .then(async body => {
        const url = 'http://'+(req.headers.host ?? "localhost")+req.url;
        const request = new Request(url, {
          method: req.method ?? "GET",
          headers: req.headers as Record<string, string>,
          body: ["GET", "HEAD"].includes(req.method || "") ? undefined : body,
        });
        return fetch(request);
      })
      .then(restateResponse => {
        resp.writeHead(
          restateResponse.status,
          Object.fromEntries(restateResponse.headers.entries()),
        );
        if (!restateResponse.body) {
          resp.end();
          return;
        }
        return restateResponse.body.getReader();
      })
      .then(reader => {
        if (!reader) return;
        const pump = (): Promise<void> => reader.read()
          .then(({done, value}) => {
            if (done) {
              resp.end();
              return;
            }
            resp.write(value);
            return pump();
          });
        return pump();
      })
      .catch(err => {
        console.error(err);
        resp.writeHead(500, { "Content-Type": "text/plain" });
        resp.end(String(err));
      });
  };
}

/**
 * Utility to read the entire request body from Encore's IncomingMessage.
 * Returns a string, but you could change it to return a Buffer if needed.
 */
function getBody(req: IncomingMessage): Promise<Buffer> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk) => chunks.push(chunk));
    req.on("end", () => {
      try {
        resolve(Buffer.concat(chunks));
      } catch (err) {
        reject(err);
      }
    });
    req.on("error", (err) => reject(err));
  });
}`
	rootIndexPath := filepath.Join(restDir, "index.ts")
	if err := ioutil.WriteFile(rootIndexPath, []byte(rootIndexContent), 0644); err != nil {
		return fmt.Errorf("error writing root restate.gen index: %v", err)
	}
	return nil
}

// cleanDanglingGeneratedFiles scans the project and removes any generated file ending with .restate.ts
// in a service directory where no valid handlers are found.
func cleanDanglingGeneratedFiles(root, suffix string) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), suffix) {
			dir := filepath.Dir(path)
			manifest, err := runNodeScript(dir)
			if err != nil {
				return nil
			}
			if len(manifest.Handlers) == 0 {
				os.Remove(path)
				log.Printf("Removed generated file: %s", path)
				// Remove any stored TemplateData for this directory.
				generatedDataMapMutex.Lock()
				delete(generatedDataMap, dir)
				generatedDataMapMutex.Unlock()
			}
		}
		return nil
	})
}

// initialScan walks the project and processes every directory that contains an encore.service.ts.
func initialScan(root string) {
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && !strings.Contains(path, "node_modules") &&
			!strings.Contains(path, ".gen") &&
			!strings.Contains(path, "dist") &&
			!strings.Contains(path, ".build") &&
			!strings.Contains(path, "restate.gen") {
			serviceFile := filepath.Join(path, "encore.service.ts")
			if _, err := os.Stat(serviceFile); err == nil {
				processDirectory(path)
			}
		}
		return nil
	})
}

var (
	debounceMap   = make(map[string]*time.Timer)
	debounceMutex sync.Mutex
	eventCache    sync.Map // key: file path, value: time.Time
)

func main() {
	var root string
	if len(os.Args) > 1 {
		root = os.Args[1]
	} else {
		var err error
		root, err = os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get current directory: %v", err)
		}
	}
	// Set global project root.
	projectRoot = root
	// Detect the package manager used in the project.
	globalPackageManager = detectPackageManager(projectRoot)
	// On init, check for required ReState modules without auto-installing.
	installed, err := checkRestateModules(projectRoot)
	if err != nil {
		log.Printf("Warning: could not check ReState modules: %v", err)
		restatedModulesInstalled = false
	} else {
		restatedModulesInstalled = installed
	}
	log.Printf("Monitoring Encore project at: %s", root)

	// On startup, run a full scan.
	initialScan(root)
	cleanDanglingGeneratedFiles(root, ".restate.ts")
	if err := generateCentralIndex(root); err != nil {
		log.Printf("Error generating central index: %v", err)
	}

	// Update tsconfig.json with the required paths and include rules.
	if err := updateTsConfig(projectRoot); err != nil {
		log.Printf("Error updating tsconfig.json: %v", err)
	}

	// Set up file watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// If a new directory is created, add it to the watcher.
				if event.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						if err := watcher.Add(event.Name); err != nil {
							log.Printf("Error adding new directory %s to watcher: %v", event.Name, err)
						}
						// Optionally, process the new directory if it might contain an encore.service.ts.
						processDirectory(event.Name)
						continue // Skip further file processing for directories.
					}
				}

				if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
					// Existing file handling logic (only for .ts files and valid paths)
					if strings.HasSuffix(event.Name, ".ts") &&
						!strings.Contains(event.Name, "node_modules") &&
						!strings.Contains(event.Name, ".restate.ts") &&
						!strings.Contains(event.Name, ".gen") &&
						!strings.Contains(event.Name, "dist") &&
						!strings.Contains(event.Name, ".build") &&
						!strings.Contains(event.Name, "restate.gen") {

						// Check for duplicate events for this file.
						if lastRaw, ok := eventCache.Load(event.Name); ok {
							lastTime := lastRaw.(time.Time)
							if time.Since(lastTime) < 100*time.Millisecond {
								continue // skip duplicate event
							}
						}
						eventCache.Store(event.Name, time.Now())

						dir := filepath.Dir(event.Name)
						log.Printf("Change detected: %s", event.Name)
						debounceMutex.Lock()
						if timer, exists := debounceMap[dir]; exists {
							timer.Stop()
						}
						debounceMap[dir] = time.AfterFunc(100*time.Millisecond, func() {
							processDirectory(dir)
							debounceMutex.Lock()
							delete(debounceMap, dir)
							debounceMutex.Unlock()
							if err := generateCentralIndex(projectRoot); err != nil {
								log.Printf("Error generating central index: %v", err)
							}
						})
						debounceMutex.Unlock()
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Watcher error:", err)
			}
		}
	}()

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.Contains(path, "node_modules") ||
				strings.Contains(path, ".gen") ||
				strings.Contains(path, "dist") ||
				strings.Contains(path, ".build") ||
				strings.Contains(path, ".restate.ts") ||
				strings.Contains(path, "restate.gen") {
				return filepath.SkipDir
			}
			return watcher.Add(path)
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	select {}
}
