// Package render turns view models into Go source fragments through
// text/template files only. It makes no resolution decisions and imports no
// metadata or type-mapping packages — every value it needs is already present
// on the view. (The render firewall.)
package render

import (
	"embed"
	"strings"
	"text/template"

	"github.com/deploymenttheory/go-bindings-winrt/internal/codegen/emit/winrt/view"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates = template.Must(template.New("winrt").Funcs(template.FuncMap{
	"join": strings.Join,
}).ParseFS(templateFS, "templates/*.tmpl"))

func execute(name string, data any) (string, error) {
	var builder strings.Builder
	if err := templates.ExecuteTemplate(&builder, name, data); err != nil {
		return "", err
	}
	return builder.String(), nil
}

// Enum renders one enum type block.
func Enum(model view.EnumModel) (string, error) { return execute("enum", model) }

// Struct renders one struct type block.
func Struct(model view.StructModel) (string, error) { return execute("struct", model) }

// Interface renders one WinRT interface: type, IID, and vtable methods.
func Interface(model view.InterfaceModel) (string, error) { return execute("interface", model) }

// Class renders one runtime class: type, constructor, and query methods.
func Class(model view.ClassModel) (string, error) { return execute("class", model) }
