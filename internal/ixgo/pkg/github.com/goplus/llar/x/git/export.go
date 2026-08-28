// export by github.com/goplus/ixgo/cmd/qexp

package git

import (
	q "github.com/goplus/llar/x/git"

	"reflect"

	"github.com/goplus/ixgo"
)

func init() {
	ixgo.RegisterPackage(&ixgo.Package{
		Name: "git",
		Path: "github.com/goplus/llar/x/git",
		Deps: map[string]string{
			"github.com/goplus/llar/mod/module": "module",
		},
		Interfaces: map[string]reflect.Type{},
		NamedTypes: map[string]reflect.Type{},
		AliasTypes: map[string]reflect.Type{},
		Vars:       map[string]reflect.Value{},
		Funcs: map[string]reflect.Value{
			"CompareFunc": reflect.ValueOf(q.CompareFunc),
		},
		TypedConsts:   map[string]ixgo.TypedConst{},
		UntypedConsts: map[string]ixgo.UntypedConst{},
	})
}
