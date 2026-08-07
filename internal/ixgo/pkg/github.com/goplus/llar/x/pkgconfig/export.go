// export by github.com/goplus/ixgo/cmd/qexp

package pkgconfig

import (
	q "github.com/goplus/llar/x/pkgconfig"

	"reflect"

	"github.com/goplus/ixgo"
)

func init() {
	ixgo.RegisterPackage(&ixgo.Package{
		Name: "pkgconfig",
		Path: "github.com/goplus/llar/x/pkgconfig",
		Deps: map[string]string{
			"fmt": "fmt",
			"github.com/goplus/llar/internal/execbroker": "execbroker",
			"os":            "os",
			"path/filepath": "filepath",
			"strings":       "strings",
		},
		Interfaces: map[string]reflect.Type{},
		NamedTypes: map[string]reflect.Type{},
		AliasTypes: map[string]reflect.Type{},
		Vars:       map[string]reflect.Value{},
		Funcs: map[string]reflect.Value{
			"CFlags":     reflect.ValueOf(q.CFlags),
			"Libs":       reflect.ValueOf(q.Libs),
			"StaticLibs": reflect.ValueOf(q.StaticLibs),
			"Use":        reflect.ValueOf(q.Use),
		},
		TypedConsts:   map[string]ixgo.TypedConst{},
		UntypedConsts: map[string]ixgo.UntypedConst{},
	})
}
