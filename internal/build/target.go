package build

import (
	"os"

	"github.com/goplus/llar/internal/build/c"
	"github.com/goplus/llar/internal/execbroker"
)

func targetMiddleware(target *c.Target) execbroker.Middleware {
	return func(req execbroker.Request) execbroker.Request {
		env := req.Env
		if env == nil {
			env = os.Environ()
		}
		patch := target.Use(c.Command{
			Name: req.Name,
			Args: req.Args,
			Env:  env,
			Dir:  req.Dir,
		})
		if patch.Name != "" {
			req.Name = patch.Name
		}
		if len(patch.PrependArg) > 0 {
			req.Args = append(append([]string(nil), patch.PrependArg...), req.Args...)
		}
		if len(patch.AppendArg) > 0 {
			req.Args = append(req.Args, patch.AppendArg...)
		}
		if patch.Env != nil {
			req.Env = append([]string(nil), patch.Env...)
		}
		return req
	}
}
