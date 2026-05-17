package tsast_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness/scan/tsast"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func findKind(findings []tsast.Finding, kind string) []tsast.Finding {
	var out []tsast.Finding
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func findName(findings []tsast.Finding, name string) *tsast.Finding {
	for i := range findings {
		if findings[i].Name == name {
			return &findings[i]
		}
	}
	return nil
}

// ─── Component extraction ────────────────────────────────────────────────────

func TestScanFile_ExportedFunctionComponent(t *testing.T) {
	src := `
export function ServiceStatusCard({ serviceId }: { serviceId: string }) {
  return <div>{serviceId}</div>;
}
`
	path := writeTemp(t, "ServiceStatusCard.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	components := findKind(findings, tsast.KindComponent)
	if len(components) == 0 {
		t.Fatal("expected at least one component finding")
	}
	if findName(components, "ServiceStatusCard") == nil {
		t.Errorf("expected ServiceStatusCard component, got %v", components)
	}
}

func TestScanFile_ExportedConstComponent(t *testing.T) {
	src := `
export const InstallButton = ({ packageId }: Props) => {
  return <button>{packageId}</button>;
};
`
	path := writeTemp(t, "InstallButton.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if findName(findKind(findings, tsast.KindComponent), "InstallButton") == nil {
		t.Error("expected InstallButton component")
	}
}

func TestScanFile_ReactFCComponent(t *testing.T) {
	src := `
const WorkflowFailurePanel: React.FC<Props> = ({ reason }) => {
  return <div>{reason}</div>;
};
`
	path := writeTemp(t, "WorkflowFailurePanel.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if findName(findKind(findings, tsast.KindComponent), "WorkflowFailurePanel") == nil {
		t.Error("expected WorkflowFailurePanel component")
	}
}

func TestScanFile_IgnoresLowercaseFunctions(t *testing.T) {
	src := `
function helperFn() {}
export function anotherHelper() {}
const myVar = 42;
`
	path := writeTemp(t, "helpers.ts", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findKind(findings, tsast.KindComponent) {
		if f.Name == "helperFn" || f.Name == "anotherHelper" || f.Name == "myVar" {
			t.Errorf("should not emit component for lowercase name: %s", f.Name)
		}
	}
}

// ─── Backend call extraction ──────────────────────────────────────────────────

func TestScanFile_BackendClientCall(t *testing.T) {
	src := `
const client = new ObjectStoreClient();
const result = await client.getTopology();
`
	path := writeTemp(t, "page.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	calls := findKind(findings, tsast.KindBackendCall)
	if findName(calls, "ObjectStoreClient") == nil {
		t.Errorf("expected ObjectStoreClient backend_call, got %v", calls)
	}
}

func TestScanFile_FetchCall(t *testing.T) {
	src := `
const resp = await fetch("/api/status");
`
	path := writeTemp(t, "api.ts", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if findName(findKind(findings, tsast.KindBackendCall), "fetch") == nil {
		t.Error("expected fetch backend_call")
	}
}

func TestScanFile_UseQueryCall(t *testing.T) {
	src := `
const { data } = useQuery({ queryKey: ["status"], queryFn: fetchStatus });
`
	path := writeTemp(t, "hook.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if findName(findKind(findings, tsast.KindBackendCall), "useQuery") == nil {
		t.Error("expected useQuery backend_call")
	}
}

// ─── State atom extraction ────────────────────────────────────────────────────

func TestScanFile_UseStateAtom(t *testing.T) {
	src := `
const [loading, setLoading] = useState(false);
const [error, setError] = useState<string | null>(null);
`
	path := writeTemp(t, "form.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	atoms := findKind(findings, tsast.KindStateAtom)
	if len(atoms) == 0 {
		t.Error("expected at least one state_atom for useState")
	}
}

func TestScanFile_UseSelectorAtom(t *testing.T) {
	src := `
const runtimeHealth = useSelector((s) => s.runtime[serviceId]);
`
	path := writeTemp(t, "card.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if findName(findKind(findings, tsast.KindStateAtom), "useSelector") == nil {
		t.Error("expected useSelector state_atom")
	}
}

// ─── Route extraction ─────────────────────────────────────────────────────────

func TestScanFile_RoutePath(t *testing.T) {
	src := `
const routes = [
  { path: "/admin/objectstore/topology", element: <ObjectStoreTopologyPage /> },
  { path: "/admin/packages", element: <PackageCatalog /> },
];
`
	path := writeTemp(t, "router.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	routes := findKind(findings, tsast.KindRoute)
	if len(routes) == 0 {
		t.Error("expected route findings")
	}
	found := false
	for _, r := range routes {
		if r.Name == "/admin/objectstore/topology" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /admin/objectstore/topology route, got %v", routes)
	}
}

// ─── Permission check extraction ──────────────────────────────────────────────

func TestScanFile_HasPermissionCall(t *testing.T) {
	src := `
const canInstall = hasPermission("packages.install");
`
	path := writeTemp(t, "button.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(findKind(findings, tsast.KindPermissionCheck)) == 0 {
		t.Error("expected permission_check for hasPermission")
	}
}

func TestScanFile_UsePermissionHook(t *testing.T) {
	src := `
const canApply = usePermission("objectstore.topology.apply");
`
	path := writeTemp(t, "topology.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(findKind(findings, tsast.KindPermissionCheck)) == 0 {
		t.Error("expected permission_check for usePermission")
	}
}

// ─── Hook extraction ──────────────────────────────────────────────────────────

func TestScanFile_CustomHook(t *testing.T) {
	src := `
export function usePermission(action: string): boolean {
  return rbac.can(action);
}
`
	path := writeTemp(t, "usePermission.ts", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if findName(findKind(findings, tsast.KindHook), "usePermission") == nil {
		t.Error("expected usePermission hook")
	}
}

// ─── Test and story file classification ───────────────────────────────────────

func TestScanFile_TestFile(t *testing.T) {
	src := `
import { render } from "@testing-library/react";
test("renders", () => {});
`
	path := writeTemp(t, "ServiceStatusCard.test.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(findKind(findings, tsast.KindTest)) == 0 {
		t.Error("expected test finding for .test.tsx file")
	}
}

func TestScanFile_StoryFile(t *testing.T) {
	src := `
export default { title: "Components/InstallButton" };
export const Default = () => <InstallButton packageId="x" targetDomain="y" />;
`
	path := writeTemp(t, "InstallButton.stories.tsx", src)
	findings, err := tsast.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(findKind(findings, tsast.KindStory)) == 0 {
		t.Error("expected story finding for .stories.tsx file")
	}
}

// ─── Invalid / malformed TSX ──────────────────────────────────────────────────

func TestScanFile_InvalidTSXDoesNotPanic(t *testing.T) {
	src := `
<<<< INVALID TSX >>>>
export function BrokenComponent( {
  return <div unclosed
`
	path := writeTemp(t, "broken.tsx", src)
	// Must not panic; may return partial findings or empty.
	findings, _ := tsast.ScanFile(path)
	// No crash is the acceptance criterion.
	_ = findings
}

// ─── ScanDir ──────────────────────────────────────────────────────────────────

func TestScanDir_ExtractsMultipleFiles(t *testing.T) {
	// Use the frontend-app fixture directory.
	fixtureDir := filepath.Join("..", "..", "examples", "frontend-app", "src")
	if _, err := os.Stat(fixtureDir); os.IsNotExist(err) {
		t.Skip("frontend-app fixture not found, skipping ScanDir test")
	}

	findings, err := tsast.ScanDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 {
		t.Fatal("expected findings from frontend-app fixtures")
	}

	// Must find the three expected components.
	components := findKind(findings, tsast.KindComponent)
	wantComponents := []string{"ObjectStoreTopologyPage", "ServiceStatusCard", "InstallButton"}
	for _, want := range wantComponents {
		if findName(components, want) == nil {
			t.Errorf("expected component %s in scan results", want)
		}
	}

	// Must find at least one backend call.
	if len(findKind(findings, tsast.KindBackendCall)) == 0 {
		t.Error("expected at least one backend_call in frontend-app fixtures")
	}

	// Must find at least one state atom.
	if len(findKind(findings, tsast.KindStateAtom)) == 0 {
		t.Error("expected at least one state_atom in frontend-app fixtures")
	}

	// Must find at least one permission check.
	if len(findKind(findings, tsast.KindPermissionCheck)) == 0 {
		t.Error("expected at least one permission_check in frontend-app fixtures")
	}
}

func TestScanDir_SkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	nmDir := filepath.Join(dir, "node_modules", "some-package")
	if err := os.MkdirAll(nmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nmDir, "index.tsx"), []byte(`export function LibComponent() {}`), 0644); err != nil {
		t.Fatal(err)
	}

	findings, err := tsast.ScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if strings.Contains(f.File, "node_modules") {
			t.Errorf("ScanDir should skip node_modules, but found: %s", f.File)
		}
	}
}

