# XGo Classfile Guide

Use this guide for XGo and classfile syntax. Use the parent `SKILL.md` for the
current LLAR Formula API, matrix behavior, build tools, and validation process.

Do not derive package build flags or dependencies from the neutral syntax
fragments in this guide.

## Go Compatibility and Classfile Style

XGo is a Go-compatible superset. Use ordinary Go declarations, expressions,
statements, and control flow in a `.gox` file when they express the formula
clearly.

A formula is still an XGo classfile rather than a standalone Go program. Its
top-level variables and functions become members of the generated class, and
its embedded base class provides the formula API. For example, write
`func helper() { ... }` without a receiver; do not declare the generated
struct, receiver, or program entrypoint yourself.

## Contents

- [Go compatibility and classfile style](#go-compatibility-and-classfile-style)
- [Classfile model](#classfile-model)
- [LLAR classfiles](#llar-classfiles)
- [Imports and names](#imports-and-names)
- [Calls](#calls)
- [Lambdas](#lambdas)
- [Auto-properties and overloads](#auto-properties-and-overloads)
- [Class members](#class-members)
- [Literals and loops](#literals-and-loops)
- [Strings and echo](#strings-and-echo)
- [Error operators](#error-operators)
- [Generation consequences](#generation-consequences)
- [Checklist](#checklist)

## Classfile Model

An XGo classfile defines a class through a file rather than an explicit type
declaration. Top-level variables become fields, and top-level functions become
methods on the generated class.

Conceptually, a neutral classfile:

```gox
var count int

func increment() {
	count++
}
```

corresponds to a generated Go class shaped like:

```go
type Generated struct {
	count int
}

func (this *Generated) increment() {
	this.count++
}
```

A class framework registers a filename suffix, a base class, imports, and an
entrypoint. The generated class embeds that base class, so classfile code can
call its methods and access its properties without an explicit receiver.

Top-level executable statements are emitted into the generated `MainEntry`
method in source order. The framework entrypoint invokes `MainEntry` to
configure the generated class instance.

For complete implementation background, read the
[official XGo classfile documentation](https://github.com/goplus/xgo/blob/main/doc/classfile.md).

## LLAR Classfiles

LLAR registers two project classfiles:

| Filename suffix | Generated base class | Purpose |
| --- | --- | --- |
| `_llar.gox` | `formula.ModuleF` | Define a module build formula. |
| `_cmp.gox` | `cmp.CmpApp` | Define an optional version comparator. |

For LLAR formula loading, the generated class name comes from the filename
prefix before the first underscore. Keep that prefix a valid identifier and
retain the registered suffix exactly.

The base classes and their methods are domain APIs. Do not infer them from XGo
syntax. Read the API tables and source links in the parent `SKILL.md`.

## Imports and Names

Imports use Go syntax. Standard-library packages are not automatically
available:

```gox
import "strings"
```

Use the XGo spelling for every exported Go identifier: lowercase only its
first letter. For example, write `os.readFile(path)`, not `os.ReadFile(path)`:

```gox
os.readFile(path)        // calls os.ReadFile(path)
strings.trimSpace(value) // calls strings.TrimSpace(value)
object.run()             // calls object.Run()
```

Apply this rule consistently in formulas and follow current LLAR fixtures.

## Calls

Calls used as standalone statements may omit parentheses:

```gox
object.setName "value"
object.configure "first", "second"
```

Calls used as expressions require parentheses:

```gox
result := object.value()
err = object.run()
```

Parenthesized Go-style calls remain valid. Do not remove parentheses from a
call whose return value is used.

## Lambdas

Use `=>` for lambda expressions:

```gox
value => value != ""
(left, right) => left + right
=> true
(ctx, input, output) => {
	output.write(input)
}
```

Multiple parameters require parentheses. A block lambda uses ordinary Go/XGo
statements and returns according to the expected function signature.

## Auto-properties and Overloads

A zero-argument method can be used as an auto-property:

```gox
status // may resolve to this.Status()
```

Use parentheses when a value participates in ordinary call syntax or when the
API documentation presents it as a call.

XGo can overload functions and methods. Generated Go implementations commonly
use numbered suffixes such as `Method__0` and `Method__1`; XGo selects the
overload from the argument list:

```gox
object.location()
object.location(item)
```

Do not call the numbered Go names from classfile code. Use the XGo method name
shown by the domain API.

## Class Members

Top-level classfile variables and functions belong to the generated class.
They are not package-level globals or standalone functions.

This means a helper can access another class member without an explicit
receiver:

```gox
var prefix string

func qualify(value string) string {
	return prefix + value
}
```

Package-level declarations from imported packages remain package members.
Keep this distinction in mind when reasoning about state, closures, and helper
calls generated from a classfile.

## Literals and Loops

XGo supports inferred slice literals:

```gox
values := ["first", "second"]
```

Map literals can infer their type from an assignment or parameter:

```gox
settings := {"mode": "value"}
```

When inference is ambiguous, use `make` and explicit assignments:

```gox
settings := make(map[string]string)
settings["mode"] = "value"
```

Use Go range syntax or XGo `for in` syntax:

```gox
for _, value := range values {
	_ = value
}

for value in values {
	_ = value
}
```

Do not range over LLAR `target.require` or `target.options`; the Formula matrix
analyzer intentionally rejects that operation. This is a Formula constraint,
not a general XGo limitation.

## Strings and echo

XGo string interpolation uses `${...}`:

```gox
message := "value=${value}"
```

Use `echo` as the XGo print shorthand:

```gox
echo message
```

Use ordinary string and standard-library operations when they make the data
flow clearer. Import the required package explicitly.

## Error Operators

XGo supports postfix error operators:

| Syntax | Behavior |
| --- | --- |
| `call()!` | Panic when the error result is non-nil; otherwise yield the value. |
| `call()?` | Return the error from the current function when non-nil. |
| `call()?:fallback` | Use the fallback value when the call returns an error. |

These operators must match the surrounding function signature. In LLAR hooks,
prefer explicit error checks and record failures through the hook result API so
the build or test reports the correct error.

## Generation Consequences

Remember these properties when writing helpers or debugging generated code:

1. The filename determines the generated class identity and framework.
2. The generated class embeds the framework base class.
3. Top-level statements execute in `MainEntry` when the class instance is
   initialized.
4. Top-level variables and functions become fields and methods.
5. Unqualified domain calls resolve against the embedded base class.
6. Closures in top-level declarations may capture the generated class
   instance.
7. XGo overload selection hides numbered Go implementation suffixes.

Do not rewrite a classfile as though it were a normal package-level Go file.
When an error mentions generated Go, map the generated receiver, fields,
methods, and overload names back to these classfile rules.

## Checklist

Before editing a `.gox` file:

1. Confirm the registered filename suffix and base class.
2. Read the domain API from the parent `SKILL.md` and linked source.
3. Add explicit imports for standard-library packages.
4. Use command-style calls only for standalone statements.
5. Use parentheses when consuming return values.
6. Treat top-level variables and functions as generated class members.
7. Prefer explicit hook error handling over postfix error shortcuts.
8. Validate the resulting classfile through LLAR rather than assuming that a
   syntactically plausible construct maps to the intended domain API.
