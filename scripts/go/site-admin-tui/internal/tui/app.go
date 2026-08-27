package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"sshpilot/scripts/go/site-admin-tui/internal/deploy"
	"sshpilot/scripts/go/site-admin-tui/internal/domain"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type screen int

const (
	screenHome screen = iota
	screenDeploy
	screenSites
	screenDetails
	screenDoctor
	screenLogs
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldSelect
)

const (
	fieldName = iota
	fieldDomain
	fieldRuntime
	fieldSource
	fieldGitRepo
	fieldGitBranch
	fieldDeploySubdir
	fieldExistingDir
	fieldRootDir
	fieldSharedDirs
	fieldEnvFile
	fieldPort
	fieldStartCommand
	fieldComposeFile
	fieldPHPFPM
	fieldTLS
	fieldTLSEmail
)

type field struct {
	Label   string
	Kind    fieldKind
	Input   textinput.Model
	Options []string
	Index   int
	Help    string
}

type wizard struct {
	fields  []field
	cursor  int
	preview []string
	message string
}

type App struct {
	service *deploy.Service

	screen     screen
	width      int
	height     int
	homeCursor int
	homeItems  []string

	wizard wizard

	sites       []domain.SiteSpec
	sitesCursor int
	details     *deploy.SiteDetails
	doctor      *domain.DoctorReport
	logs        []string

	status string
	err    string
	busy   bool
}

type previewMsg struct {
	plan []string
	err  error
}

type deployMsg struct {
	result deploy.DeployResult
	err    error
}

type sitesMsg struct {
	sites []domain.SiteSpec
	err   error
}

type detailsMsg struct {
	details deploy.SiteDetails
	err     error
}

type doctorMsg struct {
	report domain.DoctorReport
	err    error
}

type logsMsg struct {
	lines []string
	err   error
}

type actionMsg struct {
	text string
	err  error
}

func NewApp(service *deploy.Service) tea.Model {
	return App{
		service:   service,
		screen:    screenHome,
		homeItems: []string{"Deploy Wizard", "Sites", "Execution Log", "Doctor", "Quit"},
		wizard:    newWizard(),
	}
}

func newWizard() wizard {
	makeInput := func(value string) textinput.Model {
		input := textinput.New()
		input.SetValue(value)
		input.Prompt = ""
		input.CharLimit = 256
		input.Width = 48
		return input
	}

	fields := []field{
		{Label: "Site name", Kind: fieldText, Input: makeInput(""), Help: "unique registry name"},
		{Label: "Domain", Kind: fieldText, Input: makeInput(""), Help: "public host name"},
		{Label: "Runtime", Kind: fieldSelect, Options: []string{"static", "php", "node", "python", "docker_compose"}},
		{Label: "Source", Kind: fieldSelect, Options: []string{"git", "existing_dir"}},
		{Label: "Git repo", Kind: fieldText, Input: makeInput(""), Help: "repo url or local bare repo path"},
		{Label: "Git branch", Kind: fieldText, Input: makeInput("main"), Help: "branch for git source"},
		{Label: "Deploy subdir", Kind: fieldText, Input: makeInput(""), Help: "optional subdir inside repo"},
		{Label: "Existing dir", Kind: fieldText, Input: makeInput(""), Help: "absolute path on server"},
		{Label: "Root dir", Kind: fieldText, Input: makeInput("."), Help: "relative app root inside release"},
		{Label: "Shared dirs", Kind: fieldText, Input: makeInput("storage,uploads"), Help: "comma separated"},
		{Label: "Env file", Kind: fieldText, Input: makeInput(".env"), Help: "relative path in release"},
		{Label: "Port", Kind: fieldText, Input: makeInput(""), Help: "for node/python/docker proxy"},
		{Label: "Start command", Kind: fieldText, Input: makeInput(""), Help: "for node/python"},
		{Label: "Compose file", Kind: fieldText, Input: makeInput("docker-compose.yml"), Help: "for docker compose"},
		{Label: "PHP-FPM service", Kind: fieldText, Input: makeInput("php8.2-fpm"), Help: "for php runtime"},
		{Label: "TLS", Kind: fieldSelect, Options: []string{"disabled", "enabled"}},
		{Label: "TLS email", Kind: fieldText, Input: makeInput(""), Help: "optional certbot email"},
	}
	fields[0].Input.Focus()
	return wizard{fields: fields}
}

func (a App) Init() tea.Cmd {
	return tea.SetWindowTitle("Site Admin TUI")
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.wizard.resize(max(30, a.width-28))
		return a, nil
	case previewMsg:
		a.busy = false
		if msg.err != nil {
			a.err = msg.err.Error()
			return a, nil
		}
		a.err = ""
		a.status = "Preview generated"
		a.wizard.preview = msg.plan
		return a, nil
	case deployMsg:
		a.busy = false
		if msg.err != nil {
			a.err = msg.err.Error()
			return a, nil
		}
		a.err = ""
		a.status = fmt.Sprintf("Deploy complete: %s -> %s", msg.result.Spec.Name, msg.result.CurrentPath)
		a.wizard.preview = msg.result.Plan
		return a, nil
	case sitesMsg:
		a.busy = false
		if msg.err != nil {
			a.err = msg.err.Error()
			return a, nil
		}
		a.err = ""
		a.sites = msg.sites
		if a.sitesCursor >= len(a.sites) {
			a.sitesCursor = max(0, len(a.sites)-1)
		}
		return a, nil
	case detailsMsg:
		a.busy = false
		if msg.err != nil {
			a.err = msg.err.Error()
			return a, nil
		}
		a.err = ""
		a.details = &msg.details
		a.screen = screenDetails
		return a, nil
	case doctorMsg:
		a.busy = false
		if msg.err != nil {
			a.err = msg.err.Error()
			return a, nil
		}
		a.err = ""
		a.doctor = &msg.report
		return a, nil
	case logsMsg:
		a.busy = false
		if msg.err != nil {
			a.err = msg.err.Error()
			return a, nil
		}
		a.err = ""
		a.logs = msg.lines
		return a, nil
	case actionMsg:
		a.busy = false
		if msg.err != nil {
			a.err = msg.err.Error()
			return a, nil
		}
		a.err = ""
		a.status = msg.text
		if a.screen == screenDetails && a.details != nil {
			return a, detailsCmd(a.service, a.details.Spec.Name)
		}
		return a, nil
	case tea.KeyMsg:
		switch a.screen {
		case screenHome:
			return a.updateHome(msg)
		case screenDeploy:
			return a.updateDeploy(msg)
		case screenSites:
			return a.updateSites(msg)
		case screenDetails:
			return a.updateDetails(msg)
		case screenDoctor:
			return a.updateDoctor(msg)
		case screenLogs:
			return a.updateLogs(msg)
		}
	}
	return a, nil
}

func (a App) updateHome(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.homeCursor > 0 {
			a.homeCursor--
		}
	case "down", "j":
		if a.homeCursor < len(a.homeItems)-1 {
			a.homeCursor++
		}
	case "enter":
		switch a.homeCursor {
		case 0:
			a.screen = screenDeploy
			return a, nil
		case 1:
			a.screen = screenSites
			a.busy = true
			return a, sitesCmd(a.service)
		case 2:
			a.screen = screenLogs
			a.busy = true
			return a, logsCmd(a.service)
		case 3:
			a.screen = screenDoctor
			a.busy = true
			return a, doctorCmd(a.service)
		default:
			return a, tea.Quit
		}
	case "q", "ctrl+c":
		return a, tea.Quit
	}
	return a, nil
}

func (a App) updateDeploy(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.busy {
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "esc":
			a.screen = screenHome
			a.busy = false
			return a, nil
		}
		return a, nil
	}
	switch msg.String() {
	case "esc":
		a.screen = screenHome
		a.err = ""
		return a, nil
	case "up", "k":
		a.wizard.move(-1)
	case "down", "j":
		a.wizard.move(1)
	case "left", "h":
		a.wizard.cycle(-1)
	case "right", "l":
		a.wizard.cycle(1)
	case "p":
		spec, err := a.wizard.spec()
		if err != nil {
			a.err = err.Error()
			return a, nil
		}
		a.busy = true
		return a, previewCmd(a.service, spec)
	case "d":
		spec, err := a.wizard.spec()
		if err != nil {
			a.err = err.Error()
			return a, nil
		}
		a.busy = true
		return a, deployCmd(a.service, spec)
	case "ctrl+c":
		return a, tea.Quit
	default:
		a.wizard.updateInput(msg)
	}
	return a, nil
}

func (a App) updateSites(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.screen = screenHome
		return a, nil
	case "r":
		a.busy = true
		return a, sitesCmd(a.service)
	case "up", "k":
		if a.sitesCursor > 0 {
			a.sitesCursor--
		}
	case "down", "j":
		if a.sitesCursor < len(a.sites)-1 {
			a.sitesCursor++
		}
	case "enter":
		if len(a.sites) == 0 {
			return a, nil
		}
		a.busy = true
		return a, detailsCmd(a.service, a.sites[a.sitesCursor].Name)
	case "ctrl+c":
		return a, tea.Quit
	}
	return a, nil
}

func (a App) updateDetails(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.details == nil {
		a.screen = screenSites
		return a, nil
	}
	switch msg.String() {
	case "esc":
		a.screen = screenSites
		return a, nil
	case "r":
		a.busy = true
		return a, redeployCmd(a.service, a.details.Spec.Name)
	case "b":
		a.busy = true
		return a, rollbackCmd(a.service, a.details.Spec.Name)
	case "s":
		a.busy = true
		return a, restartCmd(a.service, a.details.Spec.Name)
	case "ctrl+c":
		return a, tea.Quit
	}
	return a, nil
}

func (a App) updateDoctor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.screen = screenHome
		return a, nil
	case "r":
		a.busy = true
		return a, doctorCmd(a.service)
	case "ctrl+c":
		return a, tea.Quit
	}
	return a, nil
}

func (a App) updateLogs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.screen = screenHome
		return a, nil
	case "r":
		a.busy = true
		return a, logsCmd(a.service)
	case "ctrl+c":
		return a, tea.Quit
	}
	return a, nil
}

func (a App) View() string {
	switch a.screen {
	case screenDeploy:
		return a.renderDeploy()
	case screenSites:
		return a.renderSites()
	case screenDetails:
		return a.renderDetails()
	case screenDoctor:
		return a.renderDoctor()
	case screenLogs:
		return a.renderLogs()
	default:
		return a.renderHome()
	}
}

func (a App) renderHome() string {
	lines := []string{
		titleStyle.Render("Site Admin TUI"),
		subtitleStyle.Render("Server-side deploy/admin console for Ubuntu + nginx"),
		"",
	}
	for i, item := range a.homeItems {
		prefix := "  "
		style := itemStyle
		if i == a.homeCursor {
			prefix = accentStyle.Render("→ ")
			style = selectedStyle
		}
		lines = append(lines, prefix+style.Render(item))
	}
	lines = append(lines, "", footerStyle.Render("Enter: open  •  q: quit"))
	if a.status != "" {
		lines = append(lines, "", okStyle.Render(a.status))
	}
	if a.err != "" {
		lines = append(lines, errStyle.Render(a.err))
	}
	return rootStyle.Render(strings.Join(lines, "\n"))
}

func (a App) renderDeploy() string {
	lines := []string{
		titleStyle.Render("Deploy Wizard"),
		subtitleStyle.Render("p: preview  •  d: deploy  •  esc: home"),
		"",
	}
	for i, field := range a.wizard.visibleFields() {
		prefix := "  "
		labelStyle := itemStyle
		if i == a.wizard.visibleCursor() {
			prefix = accentStyle.Render("→ ")
			labelStyle = selectedStyle
		}
		value := a.wizard.fieldValue(field)
		lines = append(lines, fmt.Sprintf("%s%s: %s", prefix, labelStyle.Render(field.Label), value))
		if field.Help != "" && i == a.wizard.visibleCursor() {
			lines = append(lines, "    "+mutedStyle.Render(field.Help))
		}
	}
	if len(a.wizard.preview) > 0 {
		lines = append(lines, "", sectionStyle.Render("Preview"))
		for _, line := range a.wizard.preview {
			lines = append(lines, "  "+line)
		}
	}
	lines = append(lines, "", footerStyle.Render("j/k: move  •  h/l: select option  •  type: edit field"))
	if a.busy {
		lines = append(lines, warningStyle.Render("Working..."))
	}
	if a.status != "" {
		lines = append(lines, okStyle.Render(a.status))
	}
	if a.err != "" {
		lines = append(lines, errStyle.Render(a.err))
	}
	return rootStyle.Render(strings.Join(lines, "\n"))
}

func (a App) renderSites() string {
	lines := []string{
		titleStyle.Render("Sites"),
		subtitleStyle.Render("enter: details  •  r: refresh  •  esc: home"),
		"",
	}
	if len(a.sites) == 0 {
		lines = append(lines, mutedStyle.Render("No managed sites yet"))
	} else {
		for i, site := range a.sites {
			prefix := "  "
			style := itemStyle
			if i == a.sitesCursor {
				prefix = accentStyle.Render("→ ")
				style = selectedStyle
			}
			lines = append(lines, fmt.Sprintf("%s%s  %s  [%s]", prefix, style.Render(site.Name), mutedStyle.Render(site.Domain), site.Runtime))
		}
	}
	if a.busy {
		lines = append(lines, "", warningStyle.Render("Loading..."))
	}
	if a.err != "" {
		lines = append(lines, errStyle.Render(a.err))
	}
	return rootStyle.Render(strings.Join(lines, "\n"))
}

func (a App) renderDetails() string {
	if a.details == nil {
		return rootStyle.Render("No site selected")
	}
	spec := a.details.Spec
	lines := []string{
		titleStyle.Render("Site Details"),
		subtitleStyle.Render("r: redeploy  •  b: rollback  •  s: restart  •  esc: back"),
		"",
		fmt.Sprintf("Name: %s", spec.Name),
		fmt.Sprintf("Domain: %s", spec.Domain),
		fmt.Sprintf("Runtime: %s", spec.Runtime),
		fmt.Sprintf("Current: %s", a.details.CurrentPath),
		fmt.Sprintf("Source: %s", spec.Source.Kind),
	}
	lines = append(lines, "", sectionStyle.Render("History"))
	if len(a.details.History) == 0 {
		lines = append(lines, mutedStyle.Render("No releases yet"))
	} else {
		limit := min(6, len(a.details.History))
		for i := 0; i < limit; i++ {
			rec := a.details.History[i]
			lines = append(lines, fmt.Sprintf("  %s  %s  %s  %s", rec.CreatedAt.Format("2006-01-02 15:04:05"), rec.ID, rec.Status, rec.Path))
		}
	}
	if a.busy {
		lines = append(lines, "", warningStyle.Render("Working..."))
	}
	if a.status != "" {
		lines = append(lines, okStyle.Render(a.status))
	}
	if a.err != "" {
		lines = append(lines, errStyle.Render(a.err))
	}
	return rootStyle.Render(strings.Join(lines, "\n"))
}

func (a App) renderDoctor() string {
	lines := []string{
		titleStyle.Render("Doctor"),
		subtitleStyle.Render("r: rerun  •  esc: home"),
		"",
	}
	if a.doctor == nil {
		lines = append(lines, mutedStyle.Render("Doctor report not loaded"))
	} else {
		for _, check := range a.doctor.Checks {
			icon := okStyle.Render("OK")
			if !check.OK {
				icon = errStyle.Render("FAIL")
			}
			level := "optional"
			if check.Required {
				level = "required"
			}
			lines = append(lines, fmt.Sprintf("%s  %-9s %-18s %s", icon, level, check.Name, check.Detail))
		}
	}
	if a.busy {
		lines = append(lines, "", warningStyle.Render("Running checks..."))
	}
	if a.err != "" {
		lines = append(lines, errStyle.Render(a.err))
	}
	return rootStyle.Render(strings.Join(lines, "\n"))
}

func (a App) renderLogs() string {
	lines := []string{
		titleStyle.Render("Execution Log"),
		subtitleStyle.Render("r: refresh  •  esc: home"),
		"",
	}
	if len(a.logs) == 0 {
		lines = append(lines, mutedStyle.Render("Audit log is empty"))
	} else {
		lines = append(lines, a.logs...)
	}
	if a.busy {
		lines = append(lines, "", warningStyle.Render("Loading logs..."))
	}
	if a.err != "" {
		lines = append(lines, errStyle.Render(a.err))
	}
	return rootStyle.Render(strings.Join(lines, "\n"))
}

func (w *wizard) resize(width int) {
	for i := range w.fields {
		w.fields[i].Input.Width = width
	}
}

func (w *wizard) move(delta int) {
	visible := w.visibleIndices()
	pos := w.visibleCursor()
	pos += delta
	if pos < 0 {
		pos = 0
	}
	if pos >= len(visible) {
		pos = len(visible) - 1
	}
	w.setCursor(visible[pos])
}

func (w *wizard) cycle(delta int) {
	field := &w.fields[w.cursor]
	if field.Kind != fieldSelect {
		return
	}
	field.Index = (field.Index + delta + len(field.Options)) % len(field.Options)
}

func (w *wizard) updateInput(msg tea.KeyMsg) {
	field := &w.fields[w.cursor]
	if field.Kind != fieldText {
		return
	}
	model, _ := field.Input.Update(msg)
	field.Input = model
}

func (w *wizard) visibleFields() []field {
	items := make([]field, 0, len(w.fields))
	for _, idx := range w.visibleIndices() {
		items = append(items, w.fields[idx])
	}
	return items
}

func (w *wizard) visibleIndices() []int {
	runtimeType := w.fields[fieldRuntime].Options[w.fields[fieldRuntime].Index]
	sourceKind := w.fields[fieldSource].Options[w.fields[fieldSource].Index]
	indices := []int{fieldName, fieldDomain, fieldRuntime, fieldSource, fieldRootDir, fieldSharedDirs, fieldEnvFile, fieldTLS}
	if sourceKind == "git" {
		indices = append(indices, fieldGitRepo, fieldGitBranch, fieldDeploySubdir)
	} else {
		indices = append(indices, fieldExistingDir)
	}
	switch runtimeType {
	case "node", "python":
		indices = append(indices, fieldPort, fieldStartCommand)
	case "docker_compose":
		indices = append(indices, fieldPort, fieldComposeFile)
	case "php":
		indices = append(indices, fieldPHPFPM)
	}
	if w.fields[fieldTLS].Options[w.fields[fieldTLS].Index] == "enabled" {
		indices = append(indices, fieldTLSEmail)
	}
	sortInts(indices)
	return indices
}

func (w *wizard) visibleCursor() int {
	for i, idx := range w.visibleIndices() {
		if idx == w.cursor {
			return i
		}
	}
	return 0
}

func (w *wizard) setCursor(index int) {
	w.fields[w.cursor].Input.Blur()
	w.cursor = index
	if w.fields[w.cursor].Kind == fieldText {
		w.fields[w.cursor].Input.Focus()
	}
}

func (w *wizard) fieldValue(field field) string {
	if field.Kind == fieldSelect {
		return accentStyle.Render(field.Options[field.Index])
	}
	return field.Input.View()
}

func (w *wizard) spec() (domain.SiteSpec, error) {
	port := 0
	if value := strings.TrimSpace(w.fields[fieldPort].Input.Value()); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return domain.SiteSpec{}, fmt.Errorf("invalid port: %w", err)
		}
		port = parsed
	}

	spec := domain.SiteSpec{
		Name:    strings.TrimSpace(w.fields[fieldName].Input.Value()),
		Domain:  strings.TrimSpace(w.fields[fieldDomain].Input.Value()),
		Runtime: domain.RuntimeType(w.fields[fieldRuntime].Options[w.fields[fieldRuntime].Index]),
		Source: domain.DeploySource{
			Kind: domain.SourceKind(w.fields[fieldSource].Options[w.fields[fieldSource].Index]),
		},
		RootDir: strings.TrimSpace(w.fields[fieldRootDir].Input.Value()),
		EnvFile: strings.TrimSpace(w.fields[fieldEnvFile].Input.Value()),
		TLS: domain.TLSSpec{
			Enabled: w.fields[fieldTLS].Options[w.fields[fieldTLS].Index] == "enabled",
			Email:   strings.TrimSpace(w.fields[fieldTLSEmail].Input.Value()),
		},
		Service: domain.ServiceSpec{
			Port:          port,
			ComposeFile:   strings.TrimSpace(w.fields[fieldComposeFile].Input.Value()),
			PHPFPMService: strings.TrimSpace(w.fields[fieldPHPFPM].Input.Value()),
		},
	}
	if spec.Source.Kind == domain.SourceGit {
		spec.Source.Repo = strings.TrimSpace(w.fields[fieldGitRepo].Input.Value())
		spec.Source.Branch = strings.TrimSpace(w.fields[fieldGitBranch].Input.Value())
		spec.Source.DeploySubdir = strings.TrimSpace(w.fields[fieldDeploySubdir].Input.Value())
	} else {
		spec.Source.ExistingDir = strings.TrimSpace(w.fields[fieldExistingDir].Input.Value())
	}
	if command := strings.TrimSpace(w.fields[fieldStartCommand].Input.Value()); command != "" {
		spec.Service.Command = []string{command}
	}
	if shared := splitCSV(strings.TrimSpace(w.fields[fieldSharedDirs].Input.Value())); len(shared) > 0 {
		spec.SharedDirs = shared
	}
	if spec.Name == "" || spec.Domain == "" {
		return domain.SiteSpec{}, fmt.Errorf("site name and domain are required")
	}
	return spec, nil
}

func previewCmd(service *deploy.Service, spec domain.SiteSpec) tea.Cmd {
	return func() tea.Msg {
		plan, err := service.Preview(spec)
		return previewMsg{plan: plan, err: err}
	}
}

func deployCmd(service *deploy.Service, spec domain.SiteSpec) tea.Cmd {
	return func() tea.Msg {
		result, err := service.Deploy(context.Background(), spec)
		return deployMsg{result: result, err: err}
	}
}

func sitesCmd(service *deploy.Service) tea.Cmd {
	return func() tea.Msg {
		sites, err := service.ListSites()
		return sitesMsg{sites: sites, err: err}
	}
}

func detailsCmd(service *deploy.Service, site string) tea.Cmd {
	return func() tea.Msg {
		details, err := service.SiteDetails(site)
		return detailsMsg{details: details, err: err}
	}
}

func doctorCmd(service *deploy.Service) tea.Cmd {
	return func() tea.Msg {
		report, err := service.Doctor()
		return doctorMsg{report: report, err: err}
	}
}

func logsCmd(service *deploy.Service) tea.Cmd {
	return func() tea.Msg {
		lines, err := service.AuditLines(200)
		return logsMsg{lines: lines, err: err}
	}
}

func redeployCmd(service *deploy.Service, site string) tea.Cmd {
	return func() tea.Msg {
		_, err := service.Redeploy(context.Background(), site)
		return actionMsg{text: "Redeploy complete", err: err}
	}
}

func rollbackCmd(service *deploy.Service, site string) tea.Cmd {
	return func() tea.Msg {
		_, err := service.Rollback(context.Background(), site)
		return actionMsg{text: "Rollback complete", err: err}
	}
}

func restartCmd(service *deploy.Service, site string) tea.Cmd {
	return func() tea.Msg {
		err := service.Restart(context.Background(), site)
		return actionMsg{text: "Service restarted", err: err}
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func sortInts(values []int) {
	for i := range values {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var (
	rootStyle     = lipgloss.NewStyle().Padding(1, 2).Foreground(lipgloss.Color("#F8FAFC")).Background(lipgloss.Color("#0F172A"))
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E2E8F0"))
	subtitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
	itemStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7DD3FC"))
	accentStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#38BDF8"))
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#64748B"))
	footerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
	sectionStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FBBF24"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#86EFAC"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#FCA5A5"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FDE68A"))
)
