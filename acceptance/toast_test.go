//go:build windows && (amd64 || arm64)

package acceptance

import (
	"errors"
	"strings"
	"testing"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	dom "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/data/xml/dom"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui/notifications"
)

// The toast pipeline's live proof, end to end on generated code across three
// namespaces: template XML from the ToastNotificationManager statics →
// Windows.Data.Xml.Dom mutation → ToastNotification factory constructor →
// ToastNotifier. This is the first committed surface with cross-namespace
// factory parameters (IToastNotificationFactory.CreateToastNotification
// takes a Windows.Data.Xml.Dom.XmlDocument), so it also guards the
// factory-wrapper import propagation.

// acceptanceAumid is an arbitrary ApplicationUserModelId for the notifier.
// CreateToastNotifierWithId does not validate the AUMID against the registry,
// so it succeeds even for this unpackaged, unregistered test process.
const acceptanceAumid = "go-bindings-winrt.acceptance"

// toastManagerStatics fetches IToastNotificationManagerStatics.
func toastManagerStatics(t *testing.T) *notifications.IToastNotificationManagerStatics {
	t.Helper()
	statics, err := notifications.ToastNotificationManagerStatics()
	if err != nil {
		t.Fatalf("ToastNotificationManagerStatics: %v", err)
	}
	t.Cleanup(func() { statics.Release() })
	return statics
}

// templateContent fetches the ToastText01 template document.
func templateContent(t *testing.T) *dom.IXmlDocument {
	t.Helper()
	statics := toastManagerStatics(t)
	doc, err := statics.GetTemplateContent(notifications.ToastTemplateTypeToastText01)
	if err != nil {
		t.Fatalf("GetTemplateContent(ToastText01): %v", err)
	}
	if doc == nil {
		t.Fatal("GetTemplateContent returned a nil document")
	}
	t.Cleanup(func() { doc.Release() })
	return doc
}

// serializeNode reads a node's XML through IXmlNodeSerializer (GetXml lives
// there, not on IXmlDocument — the document only Requires it).
func serializeNode(t *testing.T, node unsafe.Pointer) string {
	t.Helper()
	serializer, err := winrt.QueryInterface[dom.IXmlNodeSerializer](node, &dom.IID_IXmlNodeSerializer)
	if err != nil {
		t.Fatalf("QueryInterface(IXmlNodeSerializer): %v", err)
	}
	defer serializer.Release()
	xml, err := serializer.GetXml()
	if err != nil {
		t.Fatalf("IXmlNodeSerializer.GetXml: %v", err)
	}
	return xml
}

// TestToastTemplateContentLive exercises the statics accessor, an enum
// parameter, a cross-namespace interface-pointer return, and DOM reading and
// mutation: GetElementsByTagName plus SetInnerText through the node's
// IXmlNodeSerializer.
func TestToastTemplateContentLive(t *testing.T) {
	doc := templateContent(t)

	xml := serializeNode(t, unsafe.Pointer(doc))
	if !strings.Contains(xml, "<toast") {
		t.Errorf("template XML does not contain <toast: %q", xml)
	}
	if !strings.Contains(xml, `template="ToastText01"`) {
		t.Errorf("template XML does not name ToastText01: %q", xml)
	}

	texts, err := doc.GetElementsByTagName("text")
	if err != nil {
		t.Fatalf("GetElementsByTagName(text): %v", err)
	}
	defer texts.Release()
	length, err := texts.Length()
	if err != nil {
		t.Fatalf("IXmlNodeList.Length: %v", err)
	}
	if length == 0 {
		t.Fatal("ToastText01 template has no <text> element")
	}
	node, err := texts.Item(0)
	if err != nil {
		t.Fatalf("IXmlNodeList.Item(0): %v", err)
	}
	defer node.Release()

	nodeSerializer, err := winrt.QueryInterface[dom.IXmlNodeSerializer](unsafe.Pointer(node), &dom.IID_IXmlNodeSerializer)
	if err != nil {
		t.Fatalf("QueryInterface(node IXmlNodeSerializer): %v", err)
	}
	defer nodeSerializer.Release()
	const bodyText = "go-bindings-winrt acceptance"
	if err := nodeSerializer.SetInnerText(bodyText); err != nil {
		t.Fatalf("IXmlNodeSerializer.SetInnerText: %v", err)
	}

	mutated := serializeNode(t, unsafe.Pointer(doc))
	if !strings.Contains(mutated, ">"+bodyText+"<") {
		t.Errorf("mutated template does not carry the inner text: %q", mutated)
	}
}

// TestToastNotificationFactoryLive exercises the cross-namespace factory
// constructor (CreateToastNotification takes the Data.Xml.Dom document) and
// the ExpirationTime property round-trip through a boxed IReference<DateTime>
// built with PropertyValue.CreateDateTime.
func TestToastNotificationFactoryLive(t *testing.T) {
	doc := templateContent(t)

	toast, err := notifications.CreateToastNotification(doc)
	if err != nil {
		t.Fatalf("CreateToastNotification: %v", err)
	}
	if toast == nil {
		t.Fatal("CreateToastNotification returned a nil toast")
	}
	defer toast.Release()

	content, err := toast.Content()
	if err != nil {
		t.Fatalf("IToastNotification.Content: %v", err)
	}
	if content == nil {
		t.Fatal("toast content is nil")
	}
	defer content.Release()

	// ExpirationTime is IReference<DateTime>: box a DateTime through the
	// Windows.Foundation.PropertyValue statics, re-type it to the
	// monomorphized IReferenceOfDateTime, and round-trip it.
	propertyValue, err := foundation.PropertyValueStatics()
	if err != nil {
		t.Fatalf("PropertyValueStatics: %v", err)
	}
	defer propertyValue.Release()
	const ticks = int64(143196823200000000) // an arbitrary future instant
	boxed, err := propertyValue.CreateDateTime(foundation.DateTime{UniversalTime: ticks})
	if err != nil {
		t.Fatalf("IPropertyValueStatics.CreateDateTime: %v", err)
	}
	defer boxed.Release()
	reference, err := winrt.QueryInterface[notifications.IReferenceOfDateTime](unsafe.Pointer(boxed), &notifications.IID_IReferenceOfDateTime)
	if err != nil {
		t.Fatalf("QueryInterface(IReferenceOfDateTime): %v", err)
	}
	defer reference.Release()

	if err := toast.SetExpirationTime(reference); err != nil {
		t.Fatalf("SetExpirationTime: %v", err)
	}
	back, err := toast.ExpirationTime()
	if err != nil {
		t.Fatalf("ExpirationTime: %v", err)
	}
	if back == nil {
		t.Fatal("ExpirationTime is nil after SetExpirationTime")
	}
	defer back.Release()
	value, err := back.Value()
	if err != nil {
		t.Fatalf("IReferenceOfDateTime.Value: %v", err)
	}
	if value.UniversalTime != ticks {
		t.Errorf("ExpirationTime round-trip: %d -> %d", ticks, value.UniversalTime)
	}
}

// TestToastNotifierLive exercises the notifier surface from an unpackaged
// process. CreateToastNotifier() (no AUMID) needs package identity and fails
// here — the error must still be a well-formed HRESULT. Verified live on this
// machine: CreateToastNotifierWithId succeeds for an arbitrary AUMID, and
// Show/Hide return S_OK even though the unregistered AUMID means no toast is
// visibly displayed. Elevated or stripped-down environments may refuse Show
// (the notification platform reports per-app settings failures as HRESULTs);
// that shape is tolerated as a skip, never a silent pass.
func TestToastNotifierLive(t *testing.T) {
	statics := toastManagerStatics(t)

	// Identity-less creation: unpackaged processes get 0x80070490 ("Element
	// not found"). A packaged run would succeed, so both shapes pass — what
	// must never happen is a failure that is not a well-formed HRESULT.
	if bare, err := statics.CreateToastNotifier(); err != nil {
		var hresult win32.HRESULT
		if !errors.As(err, &hresult) {
			t.Errorf("CreateToastNotifier error is not an HRESULT: %v", err)
		} else {
			t.Logf("CreateToastNotifier without identity: %v (expected unpackaged)", err)
		}
	} else {
		bare.Release()
		t.Log("CreateToastNotifier without identity succeeded (packaged context)")
	}

	notifier, err := statics.CreateToastNotifierWithId(acceptanceAumid)
	if err != nil {
		t.Fatalf("CreateToastNotifierWithId(%s): %v", acceptanceAumid, err)
	}
	if notifier == nil {
		t.Fatal("CreateToastNotifierWithId returned a nil notifier")
	}
	defer notifier.Release()

	doc := templateContent(t)
	toast, err := notifications.CreateToastNotification(doc)
	if err != nil {
		t.Fatalf("CreateToastNotification: %v", err)
	}
	defer toast.Release()

	if err := notifier.Show(&toast.IToastNotification); err != nil {
		var hresult win32.HRESULT
		if !errors.As(err, &hresult) {
			t.Fatalf("Show error is not a well-formed HRESULT: %v", err)
		}
		t.Skipf("Show refused on this host: %v (notification platform restriction; verified passing on Windows 11 Enterprise)", err)
	}
	// Shown (S_OK) — the unregistered AUMID keeps it out of the visible
	// Action Center UI, but the notification is queued: remove it again.
	if err := notifier.Hide(&toast.IToastNotification); err != nil {
		t.Errorf("Hide after successful Show: %v", err)
	}
}

// TestXmlDocumentActivationLive exercises Windows.Data.Xml.Dom on its own:
// direct activation (NewXmlDocument), LoadXml through the AsXmlDocumentIO
// query method, and the GetXml round-trip.
func TestXmlDocumentActivationLive(t *testing.T) {
	document, err := dom.NewXmlDocument()
	if err != nil {
		t.Fatalf("NewXmlDocument: %v", err)
	}
	defer document.Release()

	io, err := document.AsXmlDocumentIO()
	if err != nil {
		t.Fatalf("AsXmlDocumentIO: %v", err)
	}
	defer io.Release()
	if err := io.LoadXml(`<toast launch="acceptance"/>`); err != nil {
		t.Fatalf("IXmlDocumentIO.LoadXml: %v", err)
	}

	xml := serializeNode(t, unsafe.Pointer(&document.IXmlDocument))
	if !strings.Contains(xml, "<toast") || !strings.Contains(xml, `launch="acceptance"`) {
		t.Errorf("LoadXml/GetXml round-trip mismatch: %q", xml)
	}

	element, err := document.DocumentElement()
	if err != nil {
		t.Fatalf("IXmlDocument.DocumentElement: %v", err)
	}
	if element == nil {
		t.Fatal("DocumentElement is nil after LoadXml")
	}
	defer element.Release()
	tag, err := element.TagName()
	if err != nil {
		t.Fatalf("IXmlElement.TagName: %v", err)
	}
	if tag != "toast" {
		t.Errorf("TagName = %q, want toast", tag)
	}
}
