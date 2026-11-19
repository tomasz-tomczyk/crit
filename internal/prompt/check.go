package prompt

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"text/template"
	"text/template/parse"
)

var finishHooks = [...]string{HookFinishUnresolved, HookFinishApproved}

// Check reports problems in the effective finish templates for a session mode.
func Check(globalPrompts, projectPrompts map[string]string, projectDir, homeDir, mode string, useProject bool) []Problem {
	problems := []Problem{}
	for _, hook := range finishHooks {
		t, problem := resolveFinishTemplate(globalPrompts, projectPrompts, projectDir, homeDir, hook, mode, useProject)
		if t != nil {
			if _, err := t.check(mode, hook == HookFinishApproved); err != nil {
				problem = t.problem(err)
			}
		}
		if problem != nil {
			problems = append(problems, *problem)
		}
	}
	return problems
}

// PreviewProjectFinish shows project prompts before trust using only sample data.
// Callers must still apply the trust decision before using those prompts to finish.
func PreviewProjectFinish(projectPrompts map[string]string, projectDir, mode string) string {
	var sections []string
	for _, hook := range finishHooks {
		t, problem := resolveFinishTemplate(nil, projectPrompts, projectDir, "", hook, mode, true)
		if t != nil && t.layer == LayerProject {
			text, err := t.check(mode, hook == HookFinishApproved)
			if err == nil {
				sections = append(sections, "=== "+t.hook+" ===\n"+strings.TrimRight(text, "\n"))
			} else {
				problem = t.problem(err)
			}
		}
		if problem != nil && strings.HasPrefix(problem.Source, "project:") {
			sections = append(sections, "=== "+problem.Hook+" ===\n"+problem.String())
		}
	}
	return strings.Join(sections, "\n\n")
}

func (t resolvedTemplate) check(mode string, approved bool) (string, error) {
	tmpl, err := parseTemplate(t.text, true)
	if err != nil {
		return "", err
	}
	data := sampleContext(mode, approved).TemplateData()
	if err := checkFields(tmpl, data); err != nil {
		return "", err
	}
	return executeTemplate(tmpl, data)
}

func sampleContext(mode string, approved bool) Context {
	const reviewPath = "/example/review.json"
	ctx := Context{
		Mode:                   mode,
		Approved:               approved,
		ReviewPath:             reviewPath,
		CommentsCmd:            "crit comments --json " + reviewPath,
		CommentsAllCmd:         "crit comments --json --all " + reviewPath,
		NextRoundCmd:           "crit --session example",
		SessionKey:             "example",
		UnresolvedCount:        1,
		TotalCount:             2,
		FilesWithComments:      []string{"example.go"},
		SessionStats:           &SessionStats{DurationSeconds: 60, FilesReviewed: 2, CommentsSubmitted: 1},
		PlanSlug:               "example-plan",
		CommentsUnresolvedJSON: `[{"id":"example","body":"Review comment"}]`,
		CommentsJSON:           `[{"id":"example","body":"Review comment"}]`,
		InternalSessionMode:    "files",
		PlanHook:               "plan_mode",
	}
	if mode == "diff" || mode == "story" {
		ctx.InternalSessionMode = "git"
	}
	if approved {
		ctx.UnresolvedCount = 0
		ctx.FilesWithComments = nil
		ctx.CommentsUnresolvedJSON = "[]"
	}
	return ctx
}

// fieldChecker follows statically known maps through dot, variables and
// template calls. Dynamic expressions are left to the strict sample render.
type fieldChecker struct {
	template *template.Template
	unknown  map[string]struct{}
	visited  map[string]map[*fieldShape]bool
}

// A nil shape means an expression's value cannot be inferred from the AST.
type fieldShape struct {
	fields map[string]*fieldShape
	item   *fieldShape
}

var scalarField = &fieldShape{}

func shapeOf(value any) *fieldShape {
	switch v := value.(type) {
	case map[string]any:
		fields := make(map[string]*fieldShape, len(v))
		for key, val := range v {
			fields[key] = shapeOf(val)
		}
		return &fieldShape{fields: fields}
	case []string:
		return &fieldShape{item: scalarField}
	case nil:
		return nil
	default:
		return scalarField
	}
}

type fieldScope struct {
	dot      *fieldShape
	vars     map[string]*fieldShape
	assigned map[string]bool
}

func checkFields(tmpl *template.Template, data map[string]any) error {
	c := fieldChecker{
		template: tmpl,
		unknown:  map[string]struct{}{},
		visited:  map[string]map[*fieldShape]bool{},
	}
	root := shapeOf(data)
	c.walkTemplate(tmpl.Name(), root)
	for _, t := range tmpl.Templates() {
		if c.visited[t.Name()] == nil {
			c.walkTemplate(t.Name(), root)
		}
	}
	if len(c.unknown) == 0 {
		return nil
	}
	return fmt.Errorf("unknown template fields: %s", strings.Join(slices.Sorted(maps.Keys(c.unknown)), ", "))
}

func (c *fieldChecker) walkTemplate(name string, dot *fieldShape) {
	t := c.template.Lookup(name)
	if t == nil || t.Tree == nil {
		return
	}
	visits := c.visited[name]
	if visits == nil {
		visits = make(map[*fieldShape]bool)
		c.visited[name] = visits
	}
	// The same template can be called with different dot scopes. Deduplicate
	// each scope so recursive and repeated calls do not repeat the walk.
	if visits[dot] {
		return
	}
	visits[dot] = true
	c.walk(t.Tree.Root, fieldScope{dot: dot, vars: map[string]*fieldShape{"$": dot}, assigned: map[string]bool{}})
}

func (c *fieldChecker) walk(list *parse.ListNode, scope fieldScope) {
	if list == nil {
		return
	}
	for _, node := range list.Nodes {
		switch n := node.(type) {
		case *parse.ActionNode:
			c.walkPipe(n.Pipe, scope)
		case *parse.IfNode:
			c.walkBranch(n.BranchNode, scope)
		case *parse.WithNode:
			c.walkBranch(n.BranchNode, scope)
		case *parse.RangeNode:
			c.walkBranch(n.BranchNode, scope)
		case *parse.TemplateNode:
			c.walkTemplate(n.Name, c.walkPipe(n.Pipe, scope))
		}
	}
}

func (c *fieldChecker) walkBranch(branch parse.BranchNode, scope fieldScope) {
	scoped := fieldScope{dot: scope.dot, vars: maps.Clone(scope.vars), assigned: map[string]bool{}}
	value := c.walkPipe(branch.Pipe, scoped)
	body := fieldScope{dot: scope.dot, vars: maps.Clone(scoped.vars), assigned: scoped.assigned}
	switch branch.NodeType {
	case parse.NodeWith:
		body.dot = value
	case parse.NodeRange:
		body.dot = nil
		if value != nil {
			body.dot = value.item
		}
		for _, variable := range branch.Pipe.Decl {
			body.vars[variable.Ident[0]] = nil
		}
	}
	c.walk(branch.List, body)
	c.walk(branch.ElseList, scoped)
	for name := range scoped.assigned {
		// A conditional assignment can change a variable used after the
		// branch. Its shape is no longer statically determined.
		scope.vars[name] = nil
		scope.assigned[name] = true
	}
}

func (c *fieldChecker) walkPipe(pipe *parse.PipeNode, scope fieldScope) *fieldShape {
	if pipe == nil {
		return nil
	}
	var value *fieldShape
	for _, command := range pipe.Cmds {
		args := make([]*fieldShape, len(command.Args))
		for i, arg := range command.Args {
			args[i] = c.walkArg(arg, scope)
		}
		value = nil
		if len(args) == 1 {
			value = args[0]
		}
		if id, ok := command.Args[0].(*parse.IdentifierNode); ok && id.Ident == "index" && len(args) > 2 {
			root := args[1]
			if root == nil || root.fields == nil {
				continue
			}
			value = root
			for _, arg := range command.Args[2:] {
				key, ok := arg.(*parse.StringNode)
				if !ok {
					value = nil
					break
				}
				value = c.checkPath(value, []string{key.Text}, command.String())
			}
		}
	}
	for _, variable := range pipe.Decl {
		name := variable.Ident[0]
		scope.vars[name] = value
		if pipe.IsAssign {
			scope.assigned[name] = true
		}
	}
	return value
}

func (c *fieldChecker) walkArg(arg parse.Node, scope fieldScope) *fieldShape {
	switch n := arg.(type) {
	case *parse.DotNode:
		return scope.dot
	case *parse.FieldNode:
		return c.checkPath(scope.dot, n.Ident, n.String())
	case *parse.VariableNode:
		return c.checkPath(scope.vars[n.Ident[0]], n.Ident[1:], n.String())
	case *parse.ChainNode:
		return c.checkPath(c.walkArg(n.Node, scope), n.Field, n.String())
	case *parse.PipeNode:
		return c.walkPipe(n, scope)
	}
	return nil
}

func (c *fieldChecker) checkPath(value *fieldShape, fields []string, expression string) *fieldShape {
	for _, field := range fields {
		if value == nil {
			return nil
		}
		var ok bool
		value, ok = value.fields[field]
		if !ok {
			c.unknown[expression] = struct{}{}
			return nil
		}
	}
	return value
}
