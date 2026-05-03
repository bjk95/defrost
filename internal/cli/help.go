// help.go
package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
)

// helpStyler is the subset of cliout.Printer used by the help
// renderer. Defining it locally keeps help.go independent of the
// cliout package and makes the renderer trivially testable with a
// noop styler.
type helpStyler interface {
	Bold(string) string
	Cyan(string) string
	Yellow(string) string
	Dim(string) string
}

// MakeHelpPrinter returns a Kong HelpProviderFunc that renders
// gh-style help: titled USAGE / command-group / GLOBAL FLAGS /
// EXAMPLES blocks at the root, and a leaner per-subcommand block at
// every nested node. p is used for color decisions (bold headings,
// cyan command names, yellow flag names, dim defaults).
func MakeHelpPrinter(p helpStyler) func(kong.HelpOptions, *kong.Context) error {
	return func(opts kong.HelpOptions, ctx *kong.Context) error {
		w := ctx.Stdout
		if ctx.Selected() == nil {
			return renderRootHelp(w, ctx, p)
		}
		return renderSubcommandHelp(w, ctx, p)
	}
}

func renderRootHelp(w io.Writer, ctx *kong.Context, p helpStyler) error {
	app := ctx.Model
	fmt.Fprintf(w, "%s — %s\n\n", app.Name, app.Help)
	fmt.Fprintf(w, "%s\n  %s [global flags] <command> [flags]\n\n", p.Bold("USAGE"), app.Name)

	// Build a title map from the Group metadata attached to each child.
	// kong.Groups() option sets Group.Title; we collect it the first
	// time we see each key so the map is populated before rendering.
	titles := map[string]string{}
	groups := map[string][]*kong.Node{}
	var order []string
	for _, c := range app.Children {
		if c.Type != kong.CommandNode {
			continue
		}
		key := ""
		if c.Group != nil {
			key = c.Group.Key
			if _, seen := titles[key]; !seen && c.Group.Title != "" {
				titles[key] = c.Group.Title
			}
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], c)
	}
	preferred := []string{"core", "inspection", "management"}
	sort.SliceStable(order, func(i, j int) bool {
		ii := indexOf(preferred, order[i])
		jj := indexOf(preferred, order[j])
		return ii < jj
	})
	for _, key := range order {
		title := titles[key]
		if title == "" {
			title = strings.ToUpper(key) + " COMMANDS"
			if key == "" {
				title = "COMMANDS"
			}
		}
		fmt.Fprintf(w, "%s\n", p.Bold(title))
		for _, c := range groups[key] {
			fmt.Fprintf(w, "  %-12s %s\n", p.Cyan(c.Name+":"), oneLine(c.Help))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, p.Bold("GLOBAL FLAGS"))
	writeFlagBlock(w, p, app.Flags)
	fmt.Fprintln(w)

	fmt.Fprintln(w, p.Bold("EXAMPLES"))
	fmt.Fprintln(w, "  $ defrost exec go test ./...")
	fmt.Fprintln(w, "  $ defrost history github.com/you/pkg.TestThing")
	fmt.Fprintln(w, "  $ defrost serve")
	fmt.Fprintln(w)

	fmt.Fprintln(w, p.Bold("LEARN MORE"))
	fmt.Fprintf(w, "  Use '%s <command> --help' for detail on a command.\n", app.Name)
	fmt.Fprintln(w, "  Config: per-repo .defrost.toml, env vars DEFROST_*, then flags.")
	return nil
}

func renderSubcommandHelp(w io.Writer, ctx *kong.Context, p helpStyler) error {
	node := ctx.Selected()
	app := ctx.Model
	fmt.Fprintf(w, "%s %s — %s\n\n", app.Name, node.Path(), oneLine(node.Help))
	fmt.Fprintf(w, "%s\n  %s\n\n", p.Bold("USAGE"), node.Summary())

	if subs := commandChildren(node); len(subs) > 0 {
		fmt.Fprintln(w, p.Bold("COMMANDS"))
		for _, c := range subs {
			fmt.Fprintf(w, "  %-12s %s\n", p.Cyan(c.Name+":"), oneLine(c.Help))
		}
		fmt.Fprintln(w)
	}

	if local := localFlags(node, app); len(local) > 0 {
		fmt.Fprintln(w, p.Bold("FLAGS"))
		writeFlagBlock(w, p, local)
		fmt.Fprintln(w)
	}

	if ex := examplesFor(node); ex != "" {
		fmt.Fprintln(w, p.Bold("EXAMPLES"))
		for _, line := range strings.Split(strings.TrimRight(ex, "\n"), "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, p.Bold("INHERITS"))
	fmt.Fprintf(w, "  Global flags (--repo-dir, --dev, --no-color, …). Run '%s --help'.\n", app.Name)
	return nil
}

func writeFlagBlock(w io.Writer, p helpStyler, flags []*kong.Flag) {
	for _, f := range flags {
		short := "  "
		if f.Short != 0 {
			short = "-" + string(f.Short) + ","
		}
		name := "--" + f.Name
		if ph := f.PlaceHolder; ph != "" {
			name += " " + ph
		}
		help := oneLine(f.Help)
		if def := f.Default; def != "" {
			help += " " + p.Dim(fmt.Sprintf("(default %q)", def))
		}
		fmt.Fprintf(w, "  %-3s %-26s %s\n", short, p.Yellow(name), help)
	}
}

func commandChildren(n *kong.Node) []*kong.Node {
	var out []*kong.Node
	for _, c := range n.Children {
		if c.Type == kong.CommandNode {
			out = append(out, c)
		}
	}
	return out
}

func localFlags(node *kong.Node, app *kong.Application) []*kong.Flag {
	rootSet := map[string]struct{}{}
	for _, f := range app.Flags {
		rootSet[f.Name] = struct{}{}
	}
	var out []*kong.Flag
	for _, f := range node.Flags {
		if _, ok := rootSet[f.Name]; ok {
			continue
		}
		out = append(out, f)
	}
	return out
}

// examplesFor returns the EXAMPLES block text for a node, or "" if
// none. The convention is: if the node's Help string contains a
// blank-line-separated section starting with "EXAMPLES:", the
// remainder of the string (after the marker) is the examples block.
func examplesFor(n *kong.Node) string {
	const marker = "EXAMPLES:"
	idx := strings.Index(n.Help, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimLeft(n.Help[idx+len(marker):], " \n")
}

func oneLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return len(ss)
}
