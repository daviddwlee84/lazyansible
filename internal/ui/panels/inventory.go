// Package panels contains the individual TUI panel models.
package panels

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/daviddwlee84/lazyansible/internal/core"
)

type InspectInventoryMsg struct {
	Host, Group string
	FromRun     bool
}
type SetLimitMsg struct{ Limit string }

// InventoryNode is a flattened row in the inventory tree.
type InventoryNode struct {
	Kind     string // "group" | "host"
	Name     string
	Indent   int
	Expanded bool
	Parent   string
}

// InventoryPanel renders the inventory tree.
type InventoryPanel struct {
	inventory *core.Inventory
	nodes     []InventoryNode
	cursor    int
	// Track which groups are collapsed.
	collapsed map[string]bool
	width     int
	height    int
	focused   bool
	filter    listFilter
}

func NewInventoryPanel(inv *core.Inventory, width, height int) *InventoryPanel {
	p := &InventoryPanel{
		inventory: inv,
		collapsed: make(map[string]bool),
		filter:    newListFilter(),
		width:     width,
		height:    height,
	}
	p.buildNodes()
	return p
}

func (p *InventoryPanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

func (p *InventoryPanel) SetFocused(f bool)  { p.focused = f }
func (p *InventoryPanel) FilterActive() bool { return p.filter.active }

func (p *InventoryPanel) SetInventory(inv *core.Inventory) {
	selected := p.selectedNode()
	p.inventory = inv
	p.buildNodes()
	p.restoreNode(selected)
}

func (p *InventoryPanel) SelectedHost() string {
	if p.cursor >= 0 && p.cursor < len(p.nodes) && p.nodes[p.cursor].Kind == "host" {
		return p.nodes[p.cursor].Name
	}
	return ""
}

func (p *InventoryPanel) SelectedGroup() string {
	if p.cursor >= 0 && p.cursor < len(p.nodes) && p.nodes[p.cursor].Kind == "group" {
		return p.nodes[p.cursor].Name
	}
	return ""
}

func (p *InventoryPanel) selectedNode() InventoryNode {
	if p.cursor >= 0 && p.cursor < len(p.nodes) {
		return p.nodes[p.cursor]
	}
	return InventoryNode{}
}

func (p *InventoryPanel) restoreNode(node InventoryNode) {
	for i, current := range p.nodes {
		if current.Kind == node.Kind && current.Name == node.Name && current.Parent == node.Parent {
			p.cursor = i
			return
		}
	}
	p.cursor = clampCursor(p.cursor, len(p.nodes))
}

// buildNodes flattens the inventory tree into a list of renderable nodes.
func (p *InventoryPanel) buildNodes() {
	p.nodes = nil
	if p.inventory == nil {
		p.cursor = 0
		return
	}
	q := p.filter.query()
	children := make(map[string]bool)
	for _, group := range p.inventory.Groups {
		for _, child := range group.Children {
			children[child] = true
		}
	}
	visited := make(map[string]bool)
	var appendGroup func(string, string, int) bool
	appendGroup = func(name, parent string, indent int) bool {
		group := p.inventory.Groups[name]
		if group == nil || visited[name] {
			return false
		}
		visited[name] = true
		start := len(p.nodes)
		collapsed := p.collapsed[name] && q == ""
		p.nodes = append(p.nodes, InventoryNode{Kind: "group", Name: name, Parent: parent, Indent: indent, Expanded: !collapsed})
		matches := q == "" || strings.Contains(strings.ToLower(name), q)
		if !collapsed {
			for _, host := range group.Hosts {
				if matches || strings.Contains(strings.ToLower(host), q) {
					p.nodes = append(p.nodes, InventoryNode{Kind: "host", Name: host, Parent: name, Indent: indent + 1})
				}
			}
			for _, child := range group.Children {
				appendGroup(child, name, indent+1)
			}
		}
		if !matches && len(p.nodes) == start+1 {
			p.nodes = p.nodes[:start]
			return false
		}
		return true
	}
	order := append([]string(nil), p.inventory.OrderedGroups...)
	known := make(map[string]bool)
	for _, name := range order {
		known[name] = true
	}
	var remaining []string
	for name := range p.inventory.Groups {
		if !known[name] {
			remaining = append(remaining, name)
		}
	}
	sort.Strings(remaining)
	order = append(order, remaining...)
	for _, name := range order {
		if !children[name] {
			appendGroup(name, "", 0)
		}
	}
	// Also handle malformed cycles without recursing forever or hiding all rows.
	if len(p.nodes) == 0 && q == "" {
		for _, name := range order {
			appendGroup(name, "", 0)
		}
	}
	p.cursor = clampCursor(p.cursor, len(p.nodes))
}

// Update handles keyboard input for the inventory panel.
func (p *InventoryPanel) Update(msg tea.Msg) tea.Cmd {
	if !p.focused {
		return nil
	}
	if p.filter.active {
		if key, ok := msg.(tea.KeyMsg); ok && (key.String() == "up" || key.String() == "down") {
			if key.String() == "up" {
				p.cursor--
			} else {
				p.cursor++
			}
			p.cursor = clampCursor(p.cursor, len(p.nodes))
			return nil
		}
		changed, cmd := p.filter.update(msg)
		if changed {
			p.cursor = 0
			p.buildNodes()
		}
		return cmd
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if p.cursor < len(p.nodes)-1 {
				p.cursor++
			}
		case "k", "up":
			if p.cursor > 0 {
				p.cursor--
			}
		case "/":
			return p.filter.open()
		case "esc":
			selected := p.selectedNode()
			p.filter.input.SetValue("")
			p.buildNodes()
			p.restoreNode(selected)
		case "enter":
			host, group := p.SelectedHost(), p.SelectedGroup()
			if host != "" || group != "" {
				return func() tea.Msg { return InspectInventoryMsg{Host: host, Group: group} }
			}
		case "s":
			limit := p.SelectedHost()
			if limit == "" {
				limit = p.SelectedGroup()
			}
			if limit != "" {
				return func() tea.Msg { return SetLimitMsg{Limit: limit} }
			}
		case " ":
			if p.cursor < len(p.nodes) && p.nodes[p.cursor].Kind == "group" {
				name := p.nodes[p.cursor].Name
				p.collapsed[name] = !p.collapsed[name]
				p.buildNodes()
			}
		case "h", "left":
			node := p.selectedNode()
			if node.Kind == "group" && node.Expanded && p.filter.query() == "" {
				p.collapsed[node.Name] = true
				p.buildNodes()
				p.restoreNode(node)
			} else if node.Parent != "" {
				for i := p.cursor - 1; i >= 0; i-- {
					if p.nodes[i].Kind == "group" && p.nodes[i].Name == node.Parent {
						p.cursor = i
						break
					}
				}
			}
		case "l", "right":
			node := p.selectedNode()
			if node.Kind == "group" {
				if !node.Expanded {
					p.collapsed[node.Name] = false
					p.buildNodes()
					p.restoreNode(node)
				} else if p.cursor+1 < len(p.nodes) && p.nodes[p.cursor+1].Parent == node.Name {
					p.cursor++
				}
			}
		case "g", "home":
			p.cursor = 0
		case "G", "end":
			p.cursor = max(0, len(p.nodes)-1)
		}
	}
	return nil
}

// View renders the inventory panel.
func (p *InventoryPanel) View() string {
	if p.inventory == nil {
		return mutedText("No inventory loaded.\nUse -i flag or place inventory in current dir.")
	}

	var sb strings.Builder
	filterView := p.filter.view(p.width)
	sb.WriteString(filterView)
	if len(p.nodes) == 0 {
		return clipWidth(sb.String()+mutedText("No matching inventory entries."), p.width)
	}
	// title is shown in the panel border; no need to repeat it here

	// Determine visible slice.
	contentH := p.height - 4 // border + title
	if filterView != "" {
		contentH--
	}
	if contentH < 1 {
		contentH = 1
	}
	start := 0
	if p.cursor >= contentH {
		start = p.cursor - contentH + 1
	}
	end := start + contentH
	if end > len(p.nodes) {
		end = len(p.nodes)
	}

	for i := start; i < end; i++ {
		node := p.nodes[i]
		selected := i == p.cursor

		var line string
		switch node.Kind {
		case "group":
			g := p.inventory.Groups[node.Name]
			collapsed := !node.Expanded
			arrow := "▼"
			if collapsed {
				arrow = "▶"
			}
			count := len(g.Hosts)
			text := fmt.Sprintf("%s%s %s (%d)", strings.Repeat("  ", node.Indent), arrow, node.Name, count)
			if selected && p.focused {
				line = selectedGroupStyle.Render(text)
			} else {
				line = groupStyle.Render(text)
			}
		case "host":
			indent := strings.Repeat("  ", node.Indent)
			text := indent + "• " + node.Name
			if selected && p.focused {
				line = selectedHostStyle.Render(text)
			} else {
				line = hostStyle.Render(text)
			}
		}
		sb.WriteString(line + "\n")
	}

	return clipWidth(sb.String(), p.width)
}

var (
	groupStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7C3AED")).
			Bold(true)

	selectedGroupStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#06B6D4")).
				Bold(true).
				Background(lipgloss.Color("#1F2937"))

	hostStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D1D5DB"))

	selectedHostStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F9FAFB")).
				Bold(true).
				Background(lipgloss.Color("#1F2937"))
)

func mutedText(s string) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4B5563")).
		Italic(true).
		Render(s)
}
