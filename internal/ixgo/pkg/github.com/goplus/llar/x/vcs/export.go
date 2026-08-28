// export by github.com/goplus/ixgo/cmd/qexp

package vcs

import (
	q "github.com/goplus/llar/x/vcs"

	"reflect"

	"github.com/goplus/ixgo"
)

func init() {
	ixgo.RegisterPackage(&ixgo.Package{
		Name: "vcs",
		Path: "github.com/goplus/llar/x/vcs",
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
