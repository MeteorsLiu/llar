# Homebrew and Conan Artifact Research

This report studies how Homebrew and Conan package native libraries and how dependency information reaches a downstream build. The examples are `libpng` with a zlib-compatible dependency:

```text
Homebrew:
  libpng 1.6.58 on Linux
  dependency: zlib-ng-compat 2.3.3

Conan:
  libpng package from the local Conan cache
  dependency: zlib/1.3.Z
```

Both package managers keep dependency artifacts separate from the artifact that depends on them. The difference is in how usage information is exposed. Homebrew relies mainly on installed prefixes and native build-system conventions. Conan records usage information in recipe metadata and renders it into build-system-specific files.

## Homebrew

Homebrew distributes prebuilt native libraries as bottles. A bottle looks like an installed package prefix packaged into an archive. For the Linux `libpng` case, Homebrew has one bottle for `libpng` and another bottle for `zlib-ng-compat`; `libpng` does not contain zlib's files.

```text
libpng bottle
  include/
    png.h
    pngconf.h
  lib/
    libpng.*
    pkgconfig/
      libpng.pc
    cmake/
      ...
  bin/
    libpng-config
    libpng16-config
    png-fix-itxt
    pngfix
  .brew/
    libpng.rb

zlib-ng-compat bottle
  include/
    zlib.h
    zconf.h
  lib/
    libz.*
    pkgconfig/
      zlib.pc
  .brew/
    zlib-ng-compat.rb
```

The checked `x86_64_linux` bottle blobs are:

```text
libpng:
  https://ghcr.io/v2/homebrew/core/libpng/blobs/sha256:b5099e23c861337cff22c4b5a8c4f110b5e3bb0a3f5686fcd62f6377fac894b3

zlib-ng-compat:
  https://ghcr.io/v2/homebrew/core/zlib-ng-compat/blobs/sha256:eb72c27ca6918fb29af89b1287471e381e15156212ccd0e99661ee393bde872d
```

The dependency relation is carried by Homebrew metadata. The payload remains split by formula:

```text
libpng
  owns libpng headers, libraries, config scripts, pkg-config files, and formula metadata

zlib-ng-compat
  owns zlib-compatible headers, libraries, pkg-config files, and formula metadata
```

When Homebrew builds `libpng`, it first makes `zlib-ng-compat` available as a dependency prefix:

```text
zlib-ng-compat prefix
  include/zlib.h
  include/zconf.h
  lib/libz.*
  lib/pkgconfig/zlib.pc
```

Homebrew then prepares the build environment so `libpng` can discover that prefix through standard native mechanisms:

```text
PKG_CONFIG_PATH
  includes zlib-ng-compat/lib/pkgconfig

CMAKE_PREFIX_PATH
  includes the zlib-ng-compat prefix

CPPFLAGS
  may include the zlib-ng-compat include directory

LDFLAGS
  may include the zlib-ng-compat library directory
```

From the `libpng` build system's point of view, this is just a normal system dependency lookup. It can call `pkg-config zlib`, use CMake package discovery, run configure probes, or rely on compiler and linker search paths. The concrete flags eventually used by the compiler and linker come from the staged dependency prefix:

```text
compiler input:
  -I.../zlib-ng-compat/include

linker input:
  -L.../zlib-ng-compat/lib
  -lz
```

A downstream package using Homebrew's `libpng` follows the same model. Homebrew installs or stages `libpng` and its dependencies, then exposes the relevant prefixes. The downstream build may consume `libpng.pc`, CMake package files, `libpng-config`, or raw include/library search paths. Homebrew does not centralize those usage flags in a package-level usage schema; it prepares the environment and lets native build conventions do the final discovery.

## Conan

Conan packages also keep dependency files separate, but the package has an explicit metadata layer for consumers. The checked local Conan cache contains a `libpng` binary package with this dependency record:

```ini
[requires]
zlib/1.3.Z
```

The same package records the binary configuration that produced it:

```ini
[settings]
arch=armv8
build_type=Release
compiler=clang
compiler.version=18
os=Macos

[options]
api_prefix=
fPIC=True
neon=True
shared=False
```

The payloads are separate package folders:

```text
zlib package
  conaninfo.txt
  conanmanifest.txt
  include/
    zconf.h
    zlib.h
  lib/
    libz.a
  licenses/
    LICENSE

libpng package
  conaninfo.txt
  conanmanifest.txt
  include/
    png.h
    pngconf.h
    pnglibconf.h
    libpng16/
      png.h
      pngconf.h
      pnglibconf.h
  lib/
    libpng16.a
  licenses/
    LICENSE
```

The package files show the same separation as Homebrew: `libpng` owns libpng headers and libraries, while `zlib` owns zlib headers and libraries. The difference is that Conan recipes also describe how those files should be consumed.

The checked `libpng` recipe declares the dependency graph in `requirements()`:

```python
def requirements(self):
    self.requires("zlib/[>=1.2.11 <2]")
```

It declares consumer-facing usage information in `package_info()`:

```python
def package_info(self):
    major_min_version = f"{Version(self.version).major}{Version(self.version).minor}"

    self.cpp_info.set_property("cmake_find_mode", "both")
    self.cpp_info.set_property("cmake_file_name", "PNG")
    self.cpp_info.set_property("cmake_target_name", "PNG::PNG")
    self.cpp_info.set_property("pkg_config_name", "libpng")
    self.cpp_info.set_property("pkg_config_aliases", [f"libpng{major_min_version}"])

    prefix = "lib" if (is_msvc(self) or self._is_clang_cl) else ""
    suffix = major_min_version if self.settings.os == "Windows" else ""
    if is_msvc(self) or self._is_clang_cl:
        suffix += "_static" if not self.options.shared else ""
    suffix += "d" if self.settings.os == "Windows" and self.settings.build_type == "Debug" else ""
    self.cpp_info.libs = [f"{prefix}png{suffix}"]
    if self.settings.os in ["Linux", "Android", "FreeBSD", "SunOS", "AIX"]:
        self.cpp_info.system_libs.append("m")
```

For the checked non-Windows package shape, this maps to the library name `png16`, matching `lib/libpng16.a`.

The checked zlib recipe provides the corresponding usage information for zlib:

```python
def package_info(self):
    self.cpp_info.set_property("cmake_find_mode", "both")
    self.cpp_info.set_property("cmake_file_name", "ZLIB")
    self.cpp_info.set_property("cmake_target_name", "ZLIB::ZLIB")
    self.cpp_info.set_property("pkg_config_name", "zlib")

    if self.settings.os == "Windows" and self.settings.get_safe("compiler.runtime"):
        libname = "zdll" if self.options.shared else "zlib"
    else:
        libname = "z"
    self.cpp_info.libs = [libname]
```

For the checked non-Windows package shape, this maps to the library name `z`, matching `lib/libz.a`.

Conan therefore has three layers of data:

```text
package payload:
  headers and libraries in package folders

dependency graph:
  libpng requires zlib

usage metadata:
  libpng exposes PNG::PNG, libpng, and png16
  zlib exposes ZLIB::ZLIB, zlib, and z
```

The consumer does not usually read `cpp_info` directly. It selects a generator, and Conan renders the graph plus `cpp_info` into files for that build system.

For a CMake consumer, the application can write:

```cmake
find_package(PNG REQUIRED)
target_link_libraries(app PRIVATE PNG::PNG)
```

Conan resolves `libpng -> zlib`, locates both package folders, reads the `package_info()` metadata, and generates CMake package files. The generated `PNG::PNG` target carries the actual usage data: libpng include directories, libpng library directories, the `png16` library, and the transitive zlib target.

A checked generated CMake file shows this relationship directly:

```cmake
INTERFACE_INCLUDE_DIRECTORIES ".../include/libpng16"
INTERFACE_LINK_LIBRARIES "ZLIB::ZLIB"
```

The downstream CMake code links one target, but the generated target expands to native compiler and linker inputs.

For a pkg-config consumer, Conan renders the same metadata into `.pc` files. The consumer asks for:

```sh
pkg-config --cflags --libs libpng
```

The generated pkg-config data exposes include paths, library paths, library names, and dependency references. A checked generated zlib `.pc` file has this shape:

```pkgconfig
Libs: -L"${libdir}" -lz
Cflags: -I"${includedir}"
```

For an Autotools or Makefile-style consumer, Conan can render the graph into environment flags:

```sh
export CPPFLAGS="$CPPFLAGS -I.../include"
export LIBS="$LIBS -lz"
export LDFLAGS="$LDFLAGS -L.../lib"
```

The same package payload and recipe metadata can therefore be consumed through CMake targets, pkg-config files, or raw compiler/linker environment variables.

## Comparison

| Question | Homebrew | Conan |
| --- | --- | --- |
| Artifact payload | Installed headers, libraries, config files, formula metadata | Installed headers, libraries, Conan package metadata |
| Dependency files embedded in dependent artifact | No | No |
| Dependency relation | Formula and bottle metadata | Package graph and package metadata |
| Usage information | Installed files and prepared build environment | Recipe `cpp_info` rendered by generators |
| CMake consumption | Search installed prefixes and package files | Generated CMake package targets |
| pkg-config consumption | Installed `.pc` files through `PKG_CONFIG_PATH` | Generated `.pc` files |
| Makefile-style consumption | Environment and native conventions | Generated `CPPFLAGS`, `LDFLAGS`, and `LIBS` |

## Sources Checked

- Homebrew formula API for `libpng`.
- Homebrew formula API for `zlib-ng-compat`.
- Homebrew local source for build environment preparation and bottle extension behavior.
- Local Conan cache package for `libpng`.
- Local Conan cache package for `zlib`.
- Local Conan `libpng` and `zlib` recipes.
- Local Conan generated CMake, pkg-config, and Autotools dependency outputs.
