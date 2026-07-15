//go:build windows && (amd64 || arm64)

// Command toast runs the full toast-notification pipeline: fetch a template
// XML document from the ToastNotificationManager statics, set its text
// through the Windows.Data.Xml.Dom surface, wrap it in a ToastNotification
// via the factory constructor, and Show it through a notifier bound to an
// application ID.
//
// A note on visibility: Show validates and queues the notification, but
// Windows only RENDERS toasts for an AUMID (ApplicationUserModelId) that is
// registered with the shell — a packaged app, or a Start-menu shortcut /
// registry entry carrying the AUMID. From a plain unregistered process (like
// this example) Show returns S_OK and nothing pops up. The pipeline is still
// exercised end to end; register the AUMID to see the toast.
package main

import (
	"fmt"
	"log"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	dom "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/data/xml/dom"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui/notifications"
)

// appID is the AUMID the notifier is bound to. CreateToastNotifierWithId
// accepts any string — registration only matters for rendering (see above).
const appID = "go-bindings-winrt.examples.toast"

func main() {
	// The statics accessor fetches the class's activation factory queried to
	// IToastNotificationManagerStatics. Caller owns the reference.
	statics, err := notifications.ToastNotificationManagerStatics()
	if err != nil {
		log.Fatalf("ToastNotificationManagerStatics: %v", err)
	}
	defer statics.Release()

	// 1. Template XML: the OS hands back a mutable XmlDocument for the
	// requested template shape (ToastText01 = one text line).
	doc, err := statics.GetTemplateContent(notifications.ToastTemplateTypeToastText01)
	if err != nil {
		log.Fatalf("GetTemplateContent: %v", err)
	}
	defer doc.Release()

	// 2. Mutate the text node. GetXml/SetInnerText live on
	// IXmlNodeSerializer, a separate interface the node implements — query
	// for it.
	texts, err := doc.GetElementsByTagName("text")
	if err != nil {
		log.Fatalf("GetElementsByTagName(text): %v", err)
	}
	defer texts.Release()
	node, err := texts.Item(0)
	if err != nil {
		log.Fatalf("IXmlNodeList.Item(0): %v", err)
	}
	defer node.Release()
	serializer, err := winrt.QueryInterface[dom.IXmlNodeSerializer](unsafe.Pointer(node), &dom.IID_IXmlNodeSerializer)
	if err != nil {
		log.Fatalf("QueryInterface(IXmlNodeSerializer): %v", err)
	}
	defer serializer.Release()
	if err := serializer.SetInnerText("Hello from go-bindings-winrt"); err != nil {
		log.Fatalf("SetInnerText: %v", err)
	}

	docSerializer, err := winrt.QueryInterface[dom.IXmlNodeSerializer](unsafe.Pointer(doc), &dom.IID_IXmlNodeSerializer)
	if err != nil {
		log.Fatalf("QueryInterface(document IXmlNodeSerializer): %v", err)
	}
	defer docSerializer.Release()
	xml, err := docSerializer.GetXml()
	if err != nil {
		log.Fatalf("GetXml: %v", err)
	}
	fmt.Printf("toast payload: %s\n", xml)

	// 3. The ToastNotification factory constructor takes the document (a
	// cross-namespace factory parameter: notifications consumes Data.Xml.Dom).
	toast, err := notifications.CreateToastNotification(doc)
	if err != nil {
		log.Fatalf("CreateToastNotification: %v", err)
	}
	defer toast.Release()

	// 4. A notifier bound to our app ID. The identity-less
	// CreateToastNotifier() needs package identity and fails from a plain
	// process; the WithId form works anywhere.
	notifier, err := statics.CreateToastNotifierWithId(appID)
	if err != nil {
		log.Fatalf("CreateToastNotifierWithId(%s): %v", appID, err)
	}
	defer notifier.Release()

	// 5. Show. Some locked-down hosts refuse (a well-formed HRESULT) — print
	// the situation rather than crash.
	if err := notifier.Show(&toast.IToastNotification); err != nil {
		fmt.Printf("Show refused on this host: %v\n(notification platform restriction — the pipeline above still ran)\n", err)
		return
	}
	fmt.Println("toast shown (S_OK)")
	fmt.Printf("note: %q is not a registered AUMID, so Windows queues but does not render it\n", appID)

	// Cleanup: remove the queued notification again.
	if err := notifier.Hide(&toast.IToastNotification); err != nil {
		log.Fatalf("Hide: %v", err)
	}
	fmt.Println("toast hidden (cleanup)")
}
