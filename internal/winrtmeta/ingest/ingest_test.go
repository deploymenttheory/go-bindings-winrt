package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/deploymenttheory/go-bindings-winrt/internal/winrtmeta"
	winmd "github.com/deploymenttheory/go-winmd"
)

// contractFiles are the committed pins ingested by every golden test, in
// PROVENANCE.json record order.
var contractFiles = []string{
	"Windows.Foundation.FoundationContract.winmd",
	"Windows.Foundation.UniversalApiContract.winmd",
	"Windows.Networking.Connectivity.WwanContract.winmd",
}

// ingestion caches one full-union ingest across the tests (the
// UniversalApiContract is large; re-projecting per test would dominate the
// suite's runtime).
var ingestion struct {
	once       sync.Once
	namespaces []*winrtmeta.NamespaceMeta
	ingester   *Ingester
	err        error
}

func ingestAll(t *testing.T) ([]*winrtmeta.NamespaceMeta, *Ingester) {
	t.Helper()
	ingestion.once.Do(func() {
		sources := make([]Source, 0, len(contractFiles))
		for _, name := range contractFiles {
			path := filepath.Join("..", "..", "..", "metadata", "winmd", name)
			if _, err := os.Stat(path); err != nil {
				ingestion.err = err
				return
			}
			file, err := winmd.Open(path)
			if err != nil {
				ingestion.err = err
				return
			}
			sources = append(sources, Source{Name: name, File: file})
		}
		ingestion.ingester, ingestion.err = New(sources, "test")
		if ingestion.err != nil {
			return
		}
		ingestion.namespaces, ingestion.err = ingestion.ingester.Ingest()
	})
	if ingestion.err != nil {
		t.Fatalf("ingesting committed winmds: %v", ingestion.err)
	}
	return ingestion.namespaces, ingestion.ingester
}

func namespaceByName(t *testing.T, namespaces []*winrtmeta.NamespaceMeta, name string) *winrtmeta.NamespaceMeta {
	t.Helper()
	for _, meta := range namespaces {
		if meta.Namespace == name {
			return meta
		}
	}
	t.Fatalf("namespace %s not found", name)
	return nil
}

func TestIngestWholeSurface(t *testing.T) {
	namespaces, ingester := ingestAll(t)

	totalInterfaces, totalClasses, totalEnums, totalStructs, totalDelegates := 0, 0, 0, 0, 0
	for _, meta := range namespaces {
		totalInterfaces += len(meta.Interfaces)
		totalClasses += len(meta.Classes)
		totalEnums += len(meta.Enums)
		totalStructs += len(meta.Structs)
		totalDelegates += len(meta.Delegates)
	}
	t.Logf("namespaces=%d interfaces=%d classes=%d enums=%d structs=%d delegates=%d diagnostics=%d",
		len(namespaces), totalInterfaces, totalClasses, totalEnums, totalStructs, totalDelegates,
		len(ingester.Diagnostics))
	if len(namespaces) < 50 {
		t.Errorf("namespaces = %d, want >= 50", len(namespaces))
	}
	if totalInterfaces < 2000 {
		t.Errorf("interfaces = %d, want >= 2000", totalInterfaces)
	}
	if totalClasses < 1000 {
		t.Errorf("classes = %d, want >= 1000", totalClasses)
	}

	// The pinned contracts must close over themselves: an unresolved
	// TypeRef means another contract file needs pinning (WwanContract is
	// pinned for exactly this reason — UniversalApiContract references its
	// WwanConnectionProfileDetails).
	for _, diagnostic := range ingester.Diagnostics {
		if strings.HasPrefix(diagnostic, "unresolved-typeref:") {
			t.Errorf("unexpected %s", diagnostic)
		}
	}
}

func TestIngestCalendarClass(t *testing.T) {
	namespaces, _ := ingestAll(t)
	globalization := namespaceByName(t, namespaces, "Windows.Globalization")

	calendar, ok := globalization.Classes["Calendar"]
	if !ok {
		t.Fatal("Calendar class missing")
	}
	if calendar.DefaultInterface == nil {
		t.Fatal("Calendar has no [Default] interface")
	}
	if calendar.DefaultInterface.Kind != "ApiRef" || calendar.DefaultInterface.Namespace != "Windows.Globalization" ||
		calendar.DefaultInterface.Name != "ICalendar" || calendar.DefaultInterface.TargetKind != "Interface" {
		t.Errorf("DefaultInterface = %+v, want ApiRef Windows.Globalization.ICalendar (Interface)", calendar.DefaultInterface)
	}
	if !calendar.ActivatableDirect {
		t.Error("Calendar should be directly activatable")
	}
	if len(calendar.ActivatableFactories) != 2 {
		t.Fatalf("ActivatableFactories = %v, want 2 entries", calendar.ActivatableFactories)
	}
	factories := map[string]bool{}
	for _, factory := range calendar.ActivatableFactories {
		factories[factory] = true
	}
	if !factories["Windows.Globalization.ICalendarFactory"] || !factories["Windows.Globalization.ICalendarFactory2"] {
		t.Errorf("ActivatableFactories = %v, want ICalendarFactory + ICalendarFactory2", calendar.ActivatableFactories)
	}
	if calendar.Composable {
		t.Error("Calendar extends System.Object and must not be composable")
	}
}

func TestIngestICalendar(t *testing.T) {
	namespaces, _ := ingestAll(t)
	globalization := namespaceByName(t, namespaces, "Windows.Globalization")

	icalendar, ok := globalization.Interfaces["ICalendar"]
	if !ok {
		t.Fatal("ICalendar missing")
	}
	if icalendar.GUID != "ca30221d-86d9-40fb-a26b-d44eb7cf08ea" {
		t.Errorf("ICalendar GUID = %q", icalendar.GUID)
	}
	if icalendar.ExclusiveTo != "Windows.Globalization.Calendar" {
		t.Errorf("ICalendar ExclusiveTo = %q", icalendar.ExclusiveTo)
	}
	if len(icalendar.Methods) != 98 {
		t.Fatalf("ICalendar methods = %d, want 98", len(icalendar.Methods))
	}
	// MethodDef order is the vtable order: Clone is slot 6 (index 0).
	if icalendar.Methods[0].Name != "Clone" {
		t.Errorf("vtable[0] = %s, want Clone", icalendar.Methods[0].Name)
	}

	var year *winrtmeta.Property
	for i := range icalendar.Properties {
		if icalendar.Properties[i].Name == "Year" {
			year = &icalendar.Properties[i]
		}
	}
	if year == nil {
		t.Fatal("Year property missing")
	}
	if year.Getter != "get_Year" || year.Setter != "put_Year" {
		t.Errorf("Year accessors = get %q / set %q, want get_Year / put_Year", year.Getter, year.Setter)
	}
	if year.Type.Kind != "Native" || year.Type.Name != "I4" {
		t.Errorf("Year type = %+v, want Native I4", year.Type)
	}

	// Overloads share the MethodDef name; [Overload] carries the unique
	// projected name.
	var monthAsFullString *winrtmeta.Method
	for i := range icalendar.Methods {
		if icalendar.Methods[i].Overload == "MonthAsFullString" {
			monthAsFullString = &icalendar.Methods[i]
		}
	}
	if monthAsFullString == nil {
		t.Fatal("no method with Overload == MonthAsFullString")
	}
	if monthAsFullString.Name != "MonthAsString" {
		t.Errorf("MonthAsFullString metadata name = %q, want MonthAsString", monthAsFullString.Name)
	}

	// The logical signature stays un-lowered: GetDateTime returns the
	// struct directly, with no HRESULT and no synthesized retval param.
	var getDateTime *winrtmeta.Method
	for i := range icalendar.Methods {
		if icalendar.Methods[i].Name == "GetDateTime" {
			getDateTime = &icalendar.Methods[i]
		}
	}
	if getDateTime == nil {
		t.Fatal("GetDateTime missing")
	}
	if len(getDateTime.Params) != 0 {
		t.Errorf("GetDateTime params = %+v, want none (ingest must not synthesize retval)", getDateTime.Params)
	}
	if getDateTime.Return == nil || getDateTime.Return.Kind != "ApiRef" ||
		getDateTime.Return.Namespace != "Windows.Foundation" || getDateTime.Return.Name != "DateTime" ||
		getDateTime.Return.TargetKind != "Struct" {
		t.Errorf("GetDateTime return = %+v, want ApiRef Windows.Foundation.DateTime (Struct)", getDateTime.Return)
	}
}

func TestIngestDayOfWeek(t *testing.T) {
	namespaces, _ := ingestAll(t)
	globalization := namespaceByName(t, namespaces, "Windows.Globalization")

	dayOfWeek, ok := globalization.Enums["DayOfWeek"]
	if !ok {
		t.Fatal("DayOfWeek enum missing")
	}
	if dayOfWeek.BaseType != "int32" {
		t.Errorf("DayOfWeek base = %q, want int32", dayOfWeek.BaseType)
	}
	if len(dayOfWeek.Members) != 7 {
		t.Fatalf("DayOfWeek members = %d, want 7", len(dayOfWeek.Members))
	}
	if dayOfWeek.Members[0].Name != "Sunday" || dayOfWeek.Members[0].Value != "0" {
		t.Errorf("DayOfWeek[0] = %+v, want Sunday=0", dayOfWeek.Members[0])
	}
}

func TestIngestDateTimeStruct(t *testing.T) {
	namespaces, _ := ingestAll(t)
	foundation := namespaceByName(t, namespaces, "Windows.Foundation")

	dateTime, ok := foundation.Structs["DateTime"]
	if !ok {
		t.Fatal("DateTime struct missing")
	}
	if len(dateTime.Fields) != 1 {
		t.Fatalf("DateTime fields = %d, want 1", len(dateTime.Fields))
	}
	if dateTime.Fields[0].Name != "UniversalTime" || dateTime.Fields[0].Type.Kind != "Native" ||
		dateTime.Fields[0].Type.Name != "I8" {
		t.Errorf("DateTime field = %+v, want UniversalTime Native I8", dateTime.Fields[0])
	}
}

func TestIngestGenericInterfaceArity(t *testing.T) {
	namespaces, _ := ingestAll(t)
	collections := namespaceByName(t, namespaces, "Windows.Foundation.Collections")

	vector, ok := collections.Interfaces["IVector`1"]
	if !ok {
		t.Fatal("IVector`1 missing")
	}
	if vector.Arity != 1 {
		t.Errorf("IVector`1 arity = %d, want 1", vector.Arity)
	}
}

func TestIngestIDeviceWatcherEvents(t *testing.T) {
	namespaces, _ := ingestAll(t)
	enumeration := namespaceByName(t, namespaces, "Windows.Devices.Enumeration")

	watcher, ok := enumeration.Interfaces["IDeviceWatcher"]
	if !ok {
		t.Fatal("IDeviceWatcher missing")
	}
	events := map[string]*winrtmeta.Event{}
	for i := range watcher.Events {
		events[watcher.Events[i].Name] = &watcher.Events[i]
	}
	for _, name := range []string{"Added", "Updated", "Removed", "EnumerationCompleted", "Stopped"} {
		event := events[name]
		if event == nil {
			t.Errorf("event %s missing", name)
			continue
		}
		if event.AddMethod != "add_"+name || event.RemoveMethod != "remove_"+name {
			t.Errorf("event %s accessors = add %q / remove %q, want add_%s / remove_%s",
				name, event.AddMethod, event.RemoveMethod, name, name)
		}
	}
}

func TestIngestWriteReadRoundTrip(t *testing.T) {
	namespaces, _ := ingestAll(t)
	globalization := namespaceByName(t, namespaces, "Windows.Globalization")

	dir := t.TempDir()
	if err := winrtmeta.Write(dir, globalization); err != nil {
		t.Fatalf("Write: %v", err)
	}
	loaded, err := winrtmeta.Read(filepath.Join(dir, winrtmeta.FileName(globalization.Namespace)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if loaded.SchemaVersion != winrtmeta.CurrentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", loaded.SchemaVersion, winrtmeta.CurrentSchemaVersion)
	}
	if len(loaded.Interfaces) != len(globalization.Interfaces) {
		t.Errorf("round-trip interfaces = %d, want %d", len(loaded.Interfaces), len(globalization.Interfaces))
	}
	if len(loaded.Interfaces["ICalendar"].Methods) != 98 {
		t.Errorf("round-trip ICalendar methods = %d, want 98", len(loaded.Interfaces["ICalendar"].Methods))
	}
}
