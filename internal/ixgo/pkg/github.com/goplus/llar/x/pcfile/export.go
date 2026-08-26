// export by github.com/goplus/ixgo/cmd/qexp

package pcfile

import (
	q "github.com/goplus/llar/x/pcfile"

	"reflect"

	"github.com/goplus/ixgo"
)

func init() {
	ixgo.RegisterPackage(&ixgo.Package{
		Name: "pcfile",
		Path: "github.com/goplus/llar/x/pcfile",
		Deps: map[string]string{
			"fmt":                               "fmt",
			"github.com/kballard/go-shellquote": "shellquote",
			"io":                                "io",
			"path":                              "path",
			"strings":                           "strings",
		},
		Interfaces: map[string]reflect.Type{},
		NamedTypes: map[string]reflect.Type{
			"File": reflect.TypeOf((*q.File)(nil)).Elem(),
			"Spec": reflect.TypeOf((*q.Spec)(nil)).Elem(),
		},
		AliasTypes: map[string]reflect.Type{},
		Vars:       map[string]reflect.Value{},
		Funcs: map[string]reflect.Value{
			"New": reflect.ValueOf(q.New),
		},
		TypedConsts:   map[string]ixgo.TypedConst{},
		UntypedConsts: map[string]ixgo.UntypedConst{},
	})
}
