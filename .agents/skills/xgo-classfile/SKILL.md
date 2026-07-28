---
name: xgo-classfile
description: Read, write, register, or debug generic XGo classfiles and class frameworks. Use when working with ordinary .gox classes, project and work class files, class filename recognition, base classes, generated receivers and entrypoints, framework registration, or diagnostics in generated Go code.
---

# Work with XGo Classfiles

Treat the current XGo source as authoritative. Classfile behavior spans the
parser, compiler, module metadata, framework base classes, and the embedding
tool. Do not infer one framework's rules from another framework.

## Workflow

1. Identify the XGo version or checkout used by the target project.
2. Read `doc/classfile.md` in that XGo source tree for the public model.
3. Determine whether the file is an ordinary `.gox` class or belongs to a
   registered class framework.
4. For a framework class, inspect its actual project registration and base
   class package before interpreting any unqualified name.
5. Confirm filename splitting, project/work classification, generated class
   name, and entrypoint from source or a matching compiler test.
6. Make the smallest change that uses the owning framework's existing syntax
   and APIs.
7. Run the framework's existing build or test command and inspect generated Go
   only when a diagnostic requires it.

## Source Map

Use these files in the matching XGo checkout:

| Question | Source |
| --- | --- |
| Public classfile and class framework model | `doc/classfile.md` |
| File detection and `IsClass`/`IsProj` flags | `parser/parser_xgo.go` |
| Class field parsing and restrictions | `parser/parser.go` |
| Class name splitting and project/work loading | `cl/classfile.go` |
| Generated types, receivers, and entry methods | `cl/compile.go` |
| Identifier lookup inside classfiles | `cl/expr.go` |
| Embedded build-time registration | `x/build/build.go` |
| Filename and generated-code expectations | `cl/builtin_test.go`, `x/build/build_test.go`, `cl/compile_spx_test.go` |

Framework metadata uses `modfile.Project` and `modfile.Class` from
`github.com/goplus/mod/modfile`. Read the version selected by the XGo checkout,
not a different cached version.

## Ordinary `.gox` Classes

The parser treats an unregistered `.gox` file as a classfile. The compiler
derives its class name through `ClassNameAndExt`, which delegates filename
splitting to `modfile.SplitFname`. Do not implement filename splitting with a
handwritten basename rule. Current compiler tests include these cases:

| Filename | Generated class name |
| --- | --- |
| `Rect.gox` | `Rect` |
| `abc_demo.gox` | `abc` |
| `main.gox` | `_main` internally |

The first top-level `var` declaration is the class field declaration. It can
contain named fields, embedded types, pointers, qualified types, and field
tags. Class fields cannot have initialization expressions.

A top-level function without an explicit receiver becomes a method on a
pointer to the generated class. Top-level executable statements become the
class `Main` method. When automatic main generation is enabled, package
`main()` constructs the class and calls that method.

## Class Frameworks

A class framework normally has one project class and zero or more work
classes. Its registration supplies facts such as:

- project extension and base class;
- work extensions and base classes;
- package paths used for base-class and unqualified-name lookup;
- optional automatic imports;
- optional work-class prototype, prefix, and embedding behavior.

The parser asks `ClassKind` whether a filename is a framework class and whether
it is the project class. The compiler asks `LookupClass` for the corresponding
`modfile.Project`. The XGo CLI/module path and an embedding tool may wire these
callbacks differently, so inspect the path actually used by the target.

`modfile.Project.IsProj` is the reference classification rule. In the common
case where project and work classes share an extension, `main` plus that
extension is the project file and the other matching files are work files.
Frameworks can declare different extensions, so do not generalize this case.

The compiler embeds the registered project base class in the generated project
type and the registered work base class in each generated work type. A work
type may also embed a pointer to the generated project type. Project top-level
statements become `MainEntry`; the compiler generates the framework-facing
`Main` method from the registered base-class contract. Verify its signature
from the base class instead of guessing it.

Only one project file is allowed for one registered project. Multiple work
class kinds require framework metadata that identifies their prototypes.

## Classfile-Specific Syntax

Classfiles otherwise use XGo syntax. Keep language-wide features such as
command-style calls, lambdas, inferred literals, and error-wrap expressions
separate from framework APIs.

Two declarations have classfile-specific meaning:

- `func name(...)` without a receiver becomes an instance method on the
  generated class.
- `func .name(...)` declares a static method; the parser accepts this form only
  in a classfile.

For error-wrap expressions, verify behavior in `doc/docs.md` and
`cl/compile_test.go`: `expr!` panics on error, `expr?` returns the error, and
`expr?:fallback` substitutes a fallback value. Whether a framework recovers a
panic is a framework fact, not an XGo classfile guarantee.

## Debugging Generated Go

Translate generated-code diagnostics in this order:

1. Map the filename through `modfile.SplitFname` and the registered project.
2. Identify whether the file is ordinary, project, or work class code.
3. Map generated struct fields back to the first class `var` declaration.
4. Map generated receiver methods back to receiverless classfile functions.
5. Map `Main` or `MainEntry` back to top-level executable statements.
6. Resolve embedded members and unqualified names against the actual base
   class, package paths, and automatic imports.

Do not call generated helper names from source classfiles unless the framework
documents them as API. Names such as generated entry wrappers, prefixes, and
overload helpers are compiler output and may vary with the framework contract.

## Validation

Before finishing:

1. Confirm the target file is classified as intended.
2. Confirm the generated class and base class from compiler output or an
   existing golden test.
3. Confirm fields and receiverless functions generate the expected members.
4. Compile through the owning framework, not only the standalone parser.
5. Run the framework's behavioral tests.
