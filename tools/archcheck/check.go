package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Check runs every module-boundary rule over the tree rooted at fsys and
// returns the violations, sorted. modPath is the Go module path from go.mod.
//
// Rules:
//  1. the config is consistent (unique modules and packages, all D-44 prefixes owned);
//  2. every listed package exists, every Go package under internal/ belongs to a
//     module, and a module that owns tables has a migrations folder;
//  3. every module has an architecture note with the template headings (D-31);
//  4. an adapter is imported only by cmd/ or from inside that adapter's own directory, never by core packages, including its own module's (D-21);
//  5. module imports have no cycles;
//  6. restricted imports (drivers) appear only in their owning module, or in the
//     package directory (and its sub-directories) the config names (D-09);
//  7. nothing under internal/ imports cmd/ or tools/.
func Check(fsys fs.FS, modPath string, cfg Config) []string {
	c := &checker{fsys: fsys, modPath: modPath, cfg: cfg}
	c.checkConfig()
	c.checkLayout()
	c.checkNotes()
	c.checkImports()
	sort.Strings(c.out)
	return c.out
}

type checker struct {
	fsys    fs.FS
	modPath string
	cfg     Config
	out     []string
}

func (c *checker) fail(format string, a ...any) { c.out = append(c.out, fmt.Sprintf(format, a...)) }

func (c *checker) checkConfig() {
	names := map[string]bool{}
	pkgs := map[string]string{}
	owned := map[string]bool{}
	for _, m := range c.cfg.Modules {
		if names[m.Name] {
			c.fail("config: duplicate module %q", m.Name)
		}
		names[m.Name] = true
		for _, p := range m.Packages {
			if !strings.HasPrefix(p, "internal/") {
				c.fail("config: module %q package %q is not under internal/", m.Name, p)
			}
			if other, ok := pkgs[p]; ok {
				c.fail("config: package %q is owned by both %q and %q", p, other, m.Name)
			}
			pkgs[p] = m.Name
		}
		for _, t := range m.Tables {
			owned[t] = true
		}
	}
	for _, t := range c.cfg.TablePrefixes {
		if !owned[t] {
			c.fail("config: table prefix %q is owned by no module", t)
		}
	}
	for _, m := range c.cfg.Modules {
		for _, t := range m.Tables {
			if !contains(c.cfg.TablePrefixes, t) {
				c.fail("config: module %q owns unknown table prefix %q", m.Name, t)
			}
		}
	}
	for prefix, mods := range c.cfg.Restricted {
		for _, n := range mods {
			if strings.Contains(n, "/") {
				if _, ok := c.owner(n); !ok || !strings.HasPrefix(n, "internal/") {
					c.fail("config: restricted import %q names unknown package directory %q", prefix, n)
				}
				continue
			}
			if !names[n] {
				c.fail("config: restricted import %q names unknown module %q", prefix, n)
			}
		}
	}
}

// owner returns the module owning pkg (a directory relative to the root).
func (c *checker) owner(pkg string) (string, bool) {
	best, name := "", ""
	for _, m := range c.cfg.Modules {
		for _, p := range m.Packages {
			if (pkg == p || strings.HasPrefix(pkg, p+"/")) && len(p) > len(best) {
				best, name = p, m.Name
			}
		}
	}
	return name, best != ""
}

func (c *checker) isAdapter(pkg string) (module, root string, ok bool) {
	for _, m := range c.cfg.Modules {
		for _, a := range m.Adapters {
			if pkg == a || strings.HasPrefix(pkg, a+"/") {
				return m.Name, a, true
			}
		}
	}
	return "", "", false
}

func (c *checker) goDirs(root string) map[string]bool {
	dirs := map[string]bool{}
	_ = fs.WalkDir(c.fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			c.fail("walk %s: %v", p, err)
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(p, ".go") {
			dirs[path.Dir(p)] = true
		}
		return nil
	})
	return dirs
}

func (c *checker) checkLayout() {
	dirs := c.goDirs("internal")
	for _, m := range c.cfg.Modules {
		for _, p := range m.Packages {
			if !dirs[p] {
				c.fail("layout: module %q package %s has no Go files", m.Name, p)
			}
		}
		if len(m.Tables) > 0 && len(m.Packages) > 0 {
			mig := m.Packages[0] + "/migrations"
			if st, err := fs.Stat(c.fsys, mig); err != nil || !st.IsDir() {
				c.fail("layout: module %q owns tables %v but has no %s folder", m.Name, m.Tables, mig)
			}
		}
	}
	for d := range dirs {
		if _, ok := c.owner(d); !ok {
			c.fail("layout: package %s belongs to no module in the archcheck config", d)
		}
	}
}

func (c *checker) checkNotes() {
	for _, m := range c.cfg.Modules {
		p := path.Join(c.cfg.NoteDir, m.Name+".md")
		b, err := fs.ReadFile(c.fsys, p)
		if err != nil {
			c.fail("note: module %q has no architecture note at %s", m.Name, p)
			continue
		}
		for _, h := range c.cfg.NoteHeadings {
			if !strings.Contains(string(b), "\n"+h) {
				c.fail("note: %s is missing the heading %q", p, h)
			}
		}
	}
}

func (c *checker) checkImports() {
	graph := map[string]map[string]bool{}
	_ = fs.WalkDir(c.fsys, "internal", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // walk errors are reported by goDirs (checkLayout walks the same root)
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, ok := c.owner(path.Dir(p))
		if !ok {
			return nil // already reported by checkLayout
		}
		b, err := fs.ReadFile(c.fsys, p)
		if err != nil {
			c.fail("read %s: %v", p, err)
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, b, parser.ImportsOnly)
		if err != nil {
			c.fail("parse %s: %v", p, err)
			return nil
		}
		for _, spec := range f.Imports {
			imp, _ := strconv.Unquote(spec.Path.Value)
			c.checkImport(p, src, imp, graph)
		}
		return nil
	})
	c.checkCycles(graph)
}

// allowedIn reports whether file (in module src) may import something restricted to mods:
// module names, or package directories that also cover their sub-directories.
func (c *checker) allowedIn(mods []string, file, src string) bool {
	dir := path.Dir(file)
	for _, e := range mods {
		if strings.Contains(e, "/") {
			if dir == e || strings.HasPrefix(dir, e+"/") {
				return true
			}
		} else if e == src {
			return true
		}
	}
	return false
}

func (c *checker) checkImport(file, src, imp string, graph map[string]map[string]bool) {
	for prefix, mods := range c.cfg.Restricted {
		if (imp == prefix || strings.HasPrefix(imp, prefix+"/")) && !c.allowedIn(mods, file, src) {
			c.fail("import: %s (module %q) imports %q, allowed only in %v", file, src, imp, mods)
		}
	}
	rel, ok := strings.CutPrefix(imp, c.modPath+"/")
	if !ok {
		return
	}
	if strings.HasPrefix(rel, "cmd/") || strings.HasPrefix(rel, "tools/") {
		c.fail("import: %s (module %q) imports %q; internal code never imports cmd/ or tools/", file, src, imp)
		return
	}
	dst, ok := c.owner(rel)
	if !ok {
		c.fail("import: %s imports %q, which belongs to no module", file, imp)
		return
	}
	if _, root, isAdapter := c.isAdapter(rel); isAdapter {
		dir := path.Dir(file)
		if dir != root && !strings.HasPrefix(dir, root+"/") {
			c.fail("import: %s (module %q) imports adapter %q; only cmd/ may (D-21)", file, src, imp)
		}
	}
	if dst == src {
		return
	}
	if graph[src] == nil {
		graph[src] = map[string]bool{}
	}
	graph[src][dst] = true
}

func (c *checker) checkCycles(graph map[string]map[string]bool) {
	const (
		white = iota
		grey
		black
	)
	state := map[string]int{}
	var stack []string
	var visit func(string)
	visit = func(n string) {
		state[n] = grey
		stack = append(stack, n)
		next := make([]string, 0, len(graph[n]))
		for d := range graph[n] {
			next = append(next, d)
		}
		sort.Strings(next)
		for _, d := range next {
			switch state[d] {
			case white:
				visit(d)
			case grey:
				i := len(stack) - 1
				for stack[i] != d {
					i--
				}
				c.fail("import: module cycle %s -> %s", strings.Join(stack[i:], " -> "), d)
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = black
	}
	nodes := make([]string, 0, len(graph))
	for n := range graph {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if state[n] == white {
			visit(n)
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// modulePath reads the module path from the go.mod in dir.
func modulePath(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(l), "module "); ok {
			return strings.TrimSpace(p), nil
		}
	}
	return "", fmt.Errorf("no module line in %s/go.mod", dir)
}
