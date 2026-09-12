package main

import (
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// multiSelect 是一个带搜索与全选的多选列表。
//
// widget.CheckGroup 的勾选状态跟着 Options 走，一旦按搜索词过滤，
// 被过滤掉的勾选就会丢失。因此这里把选中集合独立维护，Options 只
// 决定「显示哪些」，不决定「选中哪些」。
type multiSelect struct {
	group    *widget.CheckGroup
	search   *widget.Entry
	all      []string
	selected map[string]bool
	suppress bool

	container *fyne.Container
}

func newMultiSelect(placeholder string, height float32) *multiSelect {
	m := &multiSelect{selected: map[string]bool{}}

	m.group = widget.NewCheckGroup(nil, nil)
	m.group.OnChanged = m.onChanged

	m.search = widget.NewEntry()
	m.search.SetPlaceHolder(placeholder)
	m.search.OnChanged = func(string) { m.refresh() }

	selectAll := widget.NewButton("全选", func() {
		for _, opt := range m.visibleOptions() {
			m.selected[opt] = true
		}
		m.refresh()
	})
	clearAll := widget.NewButton("全不选", func() {
		for _, opt := range m.visibleOptions() {
			delete(m.selected, opt)
		}
		m.refresh()
	})

	scroll := container.NewVScroll(m.group)
	scroll.SetMinSize(fyne.NewSize(0, height))

	m.container = container.NewBorder(
		container.NewBorder(nil, nil, nil, container.NewHBox(selectAll, clearAll), m.search),
		nil, nil, nil,
		scroll,
	)

	return m
}

// onChanged 只同步当前可见项的勾选状态，被搜索过滤掉的选中项保持不变
func (m *multiSelect) onChanged(checked []string) {
	if m.suppress {
		return
	}

	visible := map[string]bool{}
	for _, opt := range m.group.Options {
		visible[opt] = true
	}

	for opt := range m.selected {
		if visible[opt] {
			delete(m.selected, opt)
		}
	}
	for _, opt := range checked {
		m.selected[opt] = true
	}
}

func (m *multiSelect) visibleOptions() []string {
	keyword := strings.ToLower(strings.TrimSpace(m.search.Text))
	if keyword == "" {
		return m.all
	}

	var out []string
	for _, opt := range m.all {
		if strings.Contains(strings.ToLower(opt), keyword) {
			out = append(out, opt)
		}
	}
	return out
}

func (m *multiSelect) refresh() {
	options := m.visibleOptions()

	var checked []string
	for _, opt := range options {
		if m.selected[opt] {
			checked = append(checked, opt)
		}
	}

	// 重设 Options / Selected 会触发 OnChanged，此时不能让它改写选中集合
	m.suppress = true
	m.group.Options = options
	m.group.Selected = checked
	m.suppress = false

	m.group.Refresh()
}

// SetOptions 替换全部候选项，并丢弃已不存在的选中项（例如切换地区后）
func (m *multiSelect) SetOptions(options []string) {
	m.all = options

	valid := map[string]bool{}
	for _, opt := range options {
		valid[opt] = true
	}
	for opt := range m.selected {
		if !valid[opt] {
			delete(m.selected, opt)
		}
	}

	m.refresh()
}

// Selected 返回已选项，顺序稳定
func (m *multiSelect) Selected() []string {
	out := make([]string, 0, len(m.selected))
	for opt := range m.selected {
		out = append(out, opt)
	}
	sort.Strings(out)
	return out
}

func (m *multiSelect) Select(option string) {
	if option == "" {
		return
	}
	m.selected[option] = true
	m.refresh()
}

func (m *multiSelect) ClearSelection() {
	m.selected = map[string]bool{}
	m.refresh()
}
