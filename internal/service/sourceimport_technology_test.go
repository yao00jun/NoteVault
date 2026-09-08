package service

import (
	"reflect"
	"strings"
	"testing"
)

func TestSourceImportInfersProjectTechnologyFromRepositoryManifests(t *testing.T) {
	source, workspace := t.TempDir(), t.TempDir()
	workbenchWrite(t, source, "package.json", `{"dependencies":{"vue":"3.5.0","typescript":"6.0.0"}}`)
	workbenchWrite(t, source, "README.md", "# An existing repository\n")
	request := SourceImportRequest{SourceType: "folder", Source: source, Kind: "project", Name: "Repository"}
	result := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
	snapshot, err := NewTodoService().GetWorkbench(workspace, "2026-09-08")
	if err != nil || len(snapshot.Projects) != 1 {
		t.Fatalf("imported project must reconstruct from metadata: %+v, %v", snapshot, err)
	}
	project := snapshot.Projects[0]
	if !reflect.DeepEqual(project.TechStack, []string{"TypeScript", "Vue"}) || project.Status != "planned" || len(project.TaskIDs) != 0 {
		t.Fatalf("known dependencies should infer technology without invented progress: %+v", project)
	}
	if result.MetadataPath != "Projects/Repository/project.md" {
		t.Fatalf("unexpected metadata path: %+v", result)
	}
}

func TestSourceImportTechnologyRecognizesKnownManifestsConservatively(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		want          []string
	}{
		{"module/go.mod", "module example.org/project\n\ngo 1.25\n", []string{"Go"}},
		{"Cargo.toml", "[package]\nname = \"sample\"\n", []string{"Rust"}},
		{"pyproject.toml", "[project]\nname = \"sample\"\n", []string{"Python"}},
		{"requirements.txt", "# Python packages\nfastapi==0.120.0\nnumpy>=2\n", []string{"FastAPI", "NumPy", "Python"}},
		{"pom.xml", `<project><parent><groupId>org.springframework.boot</groupId></parent></project>`, []string{"Java", "Maven", "Spring Boot"}},
		{"build.gradle.kts", "plugins { kotlin(\"jvm\") version \"2.0\" }\n", []string{"Gradle", "Kotlin"}},
		{"package.json", `{"devDependencies":{"react":"19","typescript":"6"}}`, []string{"React", "TypeScript"}},
		{"package.json", `{"dependencies":{"vue":`, nil},
		{"notes.txt", "Use Vue, TypeScript, and Go someday.", nil},
		{"package.json", strings.Repeat(" ", 512<<10) + `{"dependencies":{"vue":"3"}}`, nil},
	} {
		t.Run(tc.name+"/"+strings.Join(tc.want, "+"), func(t *testing.T) {
			got := sourceInferTechnologies(tc.name, []byte(tc.content))
			if len(got) != len(tc.want) || len(got) > 0 && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("manifest inference: got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSourceImportTechnologyInferencePreservesExistingMetadata(t *testing.T) {
	source, workspace := t.TempDir(), t.TempDir()
	workbenchWrite(t, source, "package.json", `{"devDependencies":{"vue":"3.5.0","typescript":"6.0.0"}}`)
	const metadata = "---\r\nname: Repository\r\nstatus: active\r\ntechStack: [Custom]\r\n---\r\n# My edited project\r\n"
	workbenchWrite(t, workspace, "Projects/Repository/project.md", metadata)
	runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, SourceImportRequest{SourceType: "folder", Source: source, Kind: "project", Name: "Repository"})
	if sourceRead(t, workspace, "Projects/Repository/project.md") != metadata {
		t.Fatal("deterministic inference rewrote existing project metadata")
	}
}
