package tui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/crypto/ssh"

	"socprint/internal/catalog"
	"socprint/internal/config"
	"socprint/internal/credentials"
	"socprint/internal/store"
	"socprint/internal/transport"
)

var (
	accent        = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warm          = lipgloss.NewStyle().Foreground(lipgloss.Color("215"))
	muted         = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	strong        = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
)

var tabs = []string{"Print", "Printers", "Jobs", "Queues", "Help"}

type zone struct {
	y, left, right int
	action         string
	index          int
}

type asyncMsg struct {
	kind  string
	value any
	err   error
}

type detectResult struct{ route transport.Route }
type loginResult struct {
	client    *transport.SSH
	route     transport.Route
	username  string
	vaultErr  error
	recovered int
}
type submitResult struct {
	request transport.PrintRequest
	result  transport.Submission
}
type queueResult struct{ snapshot transport.QueueSnapshot }
type refreshResult struct {
	rows map[string]transport.QueueSnapshot
}
type keyResult struct {
	path     string
	public   string
	vaultErr error
}

type Model struct {
	settings          config.Settings
	printers          []catalog.Printer
	store             *store.Store
	client            transport.Client
	page              string
	accountStep       int
	accountNotice     string
	publicKey         string
	width, height     int
	selected          int
	scroll            int
	selectedTab       int
	input             []rune
	editField         string
	password          string
	keyPass           string
	keyConfirm        string
	keyPhase          int
	status            string
	statusDetail      string
	connected         bool
	loginPending      bool
	pendingCmd        tea.Cmd
	route             transport.Route
	loading           string
	err               error
	trust             *transport.TrustRequired
	search            string
	allPrinters       bool
	visiblePrinters   []catalog.Printer
	printer           catalog.Printer
	queue             string
	queueView         *transport.QueueSnapshot
	filePath          string
	fileName          string
	submissionUnknown bool
	unknownOperation  string
	jobIndex          int
	cancelConfirm     bool
	jobs              []store.Job
	zones             []zone
}

func New(settings config.Settings, printers []catalog.Printer, history *store.Store) *Model {
	model := &Model{settings: settings, printers: printers, store: history, page: "Account", status: "Checking", allPrinters: false, accountStep: accountUsername, editField: "username"}
	if settings.Username != "" {
		model.accountStep = accountPassword
		model.editField = "password"
	}
	return model
}

func (m *Model) Init() tea.Cmd {
	commands := []tea.Cmd{m.detectCmd()}
	if m.settings.Username != "" {
		if password, err := credentials.GetPassword(m.settings.Username); err == nil && password != "" {
			m.page = "Print"
			m.password = password
			m.loginPending = true
			m.loading = "Connecting"
			commands = append(commands, m.loginCmd(m.settings.Username, password, m.settings.KeyPath, false))
		} else {
			m.page = "Account"
			m.accountStep = accountPassword
			m.editField = "password"
			m.input = nil
		}
	}
	return tea.Batch(commands...)
}

func (m *Model) detectCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		result := transport.DetectNetwork(ctx)
		return asyncMsg{kind: "detect", value: detectResult{route: result.Route}, err: result.Err}
	}
}

func (m *Model) loginCmd(username, password, keyPath string, saveUsername bool) tea.Cmd {
	printers := m.printers
	history := m.store
	keyPass := m.keyPass
	return func() tea.Msg {
		client, err := transport.New(username, password, keyPath, printers, keyPass)
		if err != nil {
			return asyncMsg{kind: "login", err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 24*time.Second)
		defer cancel()
		route, err := client.Connect(ctx, true)
		if err != nil {
			var trust *transport.TrustRequired
			if errors.As(err, &trust) {
				return asyncMsg{kind: "login", value: loginResult{client: client, username: username}, err: err}
			}
			_ = client.Close()
			return asyncMsg{kind: "login", err: err}
		}
		vaultErr := credentials.SetPassword(username, password)
		if saveUsername {
			if err := config.Save(config.Settings{Username: username, KeyPath: keyPath}); err != nil && vaultErr == nil {
				vaultErr = fmt.Errorf("signed in, but local settings could not be saved: %w", err)
			}
		}
		recovered := 0
		if history != nil {
			jobs, _ := history.Uncertain(context.Background())
			ids := make([]string, 0, len(jobs))
			for _, job := range jobs {
				ids = append(ids, job.ID)
			}
			if len(ids) > 0 {
				receipts, recoverErr := client.Recover(ctx, ids)
				if recoverErr == nil {
					for id, receipt := range receipts {
						if history.Finish(context.Background(), id, "submitted", receipt.SpoolerID, "Recovered a successful server receipt.") == nil {
							recovered++
						}
					}
				}
			}
		}
		return asyncMsg{kind: "login", value: loginResult{client: client, route: route, username: username, vaultErr: vaultErr, recovered: recovered}}
	}
}

func (m *Model) submitCmd(request transport.PrintRequest) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		result, err := client.Submit(ctx, request)
		return asyncMsg{kind: "submit", value: submitResult{request: request, result: result}, err: err}
	}
}

func (m *Model) queryCmd(queue string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
		defer cancel()
		snapshot, err := client.Queue(ctx, queue)
		return asyncMsg{kind: "queue", value: queueResult{snapshot: snapshot}, err: err}
	}
}

func (m *Model) jobsRefreshCmd() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancelAll := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancelAll()
		jobs, err := m.store.List(ctx, 100)
		if err != nil {
			return asyncMsg{kind: "jobs-refresh", err: err}
		}
		queues := map[string]bool{}
		for _, job := range jobs {
			if job.State == "submitted" || job.State == "queued" {
				queues[job.Queue] = true
			}
		}
		rows := make(map[string]transport.QueueSnapshot)
		for queue := range queues {
			if ctx.Err() != nil {
				break
			}
			callCtx, cancel := context.WithTimeout(ctx, 18*time.Second)
			snapshot, queryErr := client.Queue(callCtx, queue)
			cancel()
			if queryErr == nil {
				rows[queue] = snapshot
			}
		}
		return asyncMsg{kind: "jobs-refresh", value: refreshResult{rows: rows}}
	}
}

func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			m.click(msg.X, msg.Y)
		}
		return m, m.takeCommand()
	case tea.KeyMsg:
		model, command := m.key(msg)
		if command == nil {
			command = m.takeCommand()
		}
		return model, command
	case jobsTimerMsg:
		if m.page == "Jobs" && m.connected {
			m.refreshJobs()
			return m, m.takeCommand()
		}
	case queueTimerMsg:
		if m.page == "Queues" && m.connected && m.queue != "" {
			m.loadQueue()
			return m, m.takeCommand()
		}
	case asyncMsg:
		if msg.kind != "detect" || !m.loginPending {
			m.loading = ""
		}
		if msg.kind == "detect" && (m.loginPending || m.connected) {
			if msg.err == nil {
				m.route = msg.value.(detectResult).route
			}
			return m, nil
		}
		if msg.err != nil {
			var host *transport.TrustRequired
			if errors.As(msg.err, &host) {
				if value, ok := msg.value.(loginResult); ok {
					if m.client != nil {
						_ = m.client.Close()
					}
					m.client = value.client
					m.settings.Username = value.username
				}
				m.trust = host
				m.loginPending = false
				m.err = nil
				m.page = "Account"
				m.status = "Checking"
				m.statusDetail = ""
				return m, nil
			}
			if msg.kind == "login" {
				m.handleLoginError(msg.err)
				return m, nil
			}
			m.err = msg.err
			if msg.kind == "detect" {
				m.status = "Connection unavailable"
				m.err = nil
				m.statusDetail = "Press R to retry. If you are outside NUS, connect to NUS VPN first."
			}
			if errors.Is(msg.err, transport.ErrChangedHostKey) {
				m.status = "Connection unavailable"
				m.connected = false
				m.statusDetail = "This could indicate an unsafe connection. Check SoC IT before changing trusted keys."
			}
			if msg.kind == "submit" {
				m.finishSubmission(msg.value.(submitResult), msg.err)
			}
			if msg.kind == "queue" && m.page == "Queues" {
				return m, m.queueTick()
			}
			if msg.kind == "jobs-refresh" && m.page == "Jobs" {
				return m, m.jobsTick()
			}
			if msg.kind == "cancel" {
				m.cancelConfirm = false
				return m, m.jobsTick()
			}
			return m, nil
		}
		switch msg.kind {
		case "detect":
			value := msg.value.(detectResult)
			m.route = value.route
			m.status = "Connected"
			m.statusDetail = "SSH is reachable; sign in to continue."
			m.err = nil
		case "login":
			m.loginPending = false
			value := msg.value.(loginResult)
			if m.client != nil {
				_ = m.client.Close()
			}
			m.client = value.client
			m.route = value.route
			m.connected = true
			m.status = "Connected"
			m.statusDetail = "Signed in to " + transport.UnixHost + " via " + value.route.String() + "."
			m.settings.Username = value.username
			m.password = ""
			m.keyPass = ""
			m.trust = nil
			m.err = nil
			m.page = "Print"
			m.selectedTab = 0
			m.editField = ""
			m.accountNotice = ""
			m.publicKey = ""
			m.accountStep = accountSignedIn
			if value.vaultErr != nil {
				m.statusDetail = "Signed in for this session. macOS Keychain could not save the password; you'll need to enter it next time."
			} else {
				m.statusDetail = ""
			}
			if value.recovered > 0 {
				recovered := fmt.Sprintf("Recovered %d print result(s).", value.recovered)
				if m.statusDetail == "" {
					m.statusDetail = recovered
				} else {
					m.statusDetail += " " + recovered
				}
			}
		case "submit":
			m.finishSubmission(msg.value.(submitResult), nil)
		case "queue":
			value := msg.value.(queueResult)
			snapshot := value.snapshot
			m.queueView = &snapshot
			m.queue = snapshot.Queue
			if snapshot.Route != transport.RouteUnknown {
				m.route = snapshot.Route
			}
			if m.store != nil {
				m.jobs, _ = m.store.List(context.Background(), 100)
				for i := range m.jobs {
					if m.jobs[i].Queue == snapshot.Queue && (m.jobs[i].State == "submitted" || m.jobs[i].State == "queued") {
						m.refreshJobStatus(&m.jobs[i], snapshot)
					}
				}
			}
			m.loading = ""
			return m, m.queueTick()
		case "jobs-refresh":
			if value, ok := msg.value.(refreshResult); ok {
				m.jobs, _ = m.store.List(context.Background(), 100)
				for i := range m.jobs {
					if row, found := value.rows[m.jobs[i].Queue]; found {
						m.refreshJobStatus(&m.jobs[i], row)
					}
				}
			}
			if m.page == "Jobs" {
				return m, m.jobsTick()
			}
		case "cancel":
			m.cancelConfirm = false
			if id, ok := msg.value.(string); ok {
				_ = m.store.Finish(context.Background(), id, "cancelled", "", "Cancelled from the SoC print queue.")
				m.jobs, _ = m.store.List(context.Background(), 100)
				m.err = nil
				m.statusDetail = "The owned queue job was cancelled."
				m.refreshJobs()
				return m, m.takeCommand()
			}
		case "key":
			value := msg.value.(keyResult)
			m.settings.KeyPath = value.path
			if err := config.Save(m.settings); err != nil {
				m.err = fmt.Errorf("the key was created but its location could not be saved: %w", err)
				m.accountStep = accountKeySetup
				m.editField = ""
				return m, nil
			}
			m.keyPhase = 0
			m.publicKey = value.public
			m.accountStep = accountEnrollKey
			m.editField = ""
			m.input = nil
			m.accountNotice = "Your key is ready. Register it with SoC before continuing."
			if value.vaultErr != nil {
				m.accountNotice += " Keychain could not save its passphrase, so you'll need to enter it again next time."
			}
			m.err = nil
		}
	}
	return m, nil
}

func (m *Model) takeCommand() tea.Cmd {
	command := m.pendingCmd
	m.pendingCmd = nil
	return command
}

func (m *Model) finishSubmission(value submitResult, err error) {
	if m.store == nil {
		return
	}
	if err != nil {
		state := "failed"
		message := safeError(err)
		var submitErr *transport.SubmissionError
		if errors.As(err, &submitErr) && submitErr.OutcomeUnknown {
			state = "unknown"
			message = "The connection ended after submission may have started. Check Jobs; do not resend until you inspect the queue."
			m.submissionUnknown = true
			m.unknownOperation = value.request.OperationID
			m.fileName = value.request.FileName
			m.statusDetail = "Outcome unknown. Inspect the current queue before preparing another submission."
		}
		_ = m.store.Finish(context.Background(), value.request.OperationID, state, "", message)
		m.err = errors.New(message)
		m.page = "Print"
		return
	}
	_ = m.store.Finish(context.Background(), value.result.OperationID, "submitted", value.result.SpoolerID, value.result.Output)
	if value.result.Route != transport.RouteUnknown {
		m.route = value.result.Route
	}
	m.fileName = value.request.FileName
	m.statusDetail = "SoC accepted the print command. This confirms submission, not that pages have physically printed."
	m.page = "Print"
	m.editField = "result"
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return strings.ToValidUTF8(strings.Map(func(r rune) rune {
		if r == 0 || r == 0x1b || r < 0x20 && r != '\n' && r != '\t' || r >= 0x7f && r <= 0x9f {
			return -1
		}
		return r
	}, err.Error()), "�")
}

func (m *Model) refreshJobStatus(job *store.Job, snapshot transport.QueueSnapshot) {
	if !snapshot.KnownEmpty && len(snapshot.Jobs) == 0 {
		return
	}
	needle := "document-" + job.ID + ".pdf"
	for _, entry := range snapshot.Jobs {
		if entry.Owner == job.Username && strings.Contains(entry.Raw, needle) {
			state := "queued"
			_ = m.store.Finish(context.Background(), job.ID, state, entry.ID, "The matching job is in the current server queue.")
			job.State = state
			job.SpoolerID = entry.ID
			return
		}
	}
	if snapshot.KnownEmpty || snapshot.Complete {
		_ = m.store.Finish(context.Background(), job.ID, "gone", job.SpoolerID, "The submission is no longer listed in the current queue.")
		job.State = "gone"
		job.Message = "The submission is no longer listed in the current queue."
	}
}

func (m *Model) jobsTick() tea.Cmd {
	return tea.Tick(10*time.Second, func(time.Time) tea.Msg { return jobsTimerMsg{} })
}
func (m *Model) queueTick() tea.Cmd {
	return tea.Tick(10*time.Second, func(time.Time) tea.Msg { return queueTimerMsg{} })
}

type jobsTimerMsg struct{}
type queueTimerMsg struct{}

func (m *Model) View() string {
	if m.width <= 0 {
		m.width = 80
	}
	m.zones = nil
	if m.page == "Account" || m.trust != nil {
		return m.accountView()
	}
	var lines []string
	var tabsLine strings.Builder
	column := 0
	for i, label := range tabs {
		shown := " " + label + " "
		if i == m.selectedTab {
			shown = selectedStyle.Render("["+label+"]") + " "
		}
		width := lipgloss.Width(shown)
		m.zones = append(m.zones, zone{y: 0, left: column, right: column + width + 1, action: "tab", index: i})
		tabsLine.WriteString(shown)
		column += width
	}
	lines = append(lines, tabsLine.String(), strong.Render("◇ Print @ SoC"))
	status := m.status
	if status == "" {
		status = "Checking"
	}
	lines = append(lines, muted.Render("Account: ")+m.accountLabel()+"     "+muted.Render("Network: ")+status)
	m.zones = append(m.zones, zone{y: 2, left: 0, right: m.width, action: "account"})
	if m.statusDetail != "" && (m.status == "Connection unavailable" || strings.HasPrefix(m.statusDetail, "Signed in for this session.") || strings.HasPrefix(m.statusDetail, "Recovered ")) {
		for _, line := range strings.Split(m.statusDetail, "\n") {
			lines = append(lines, muted.Render(safeDisplay(line)))
		}
	}
	if m.status == "Connection unavailable" {
		m.add(&lines, "[ Retry connection ]", "retry", 0)
	}
	lines = append(lines, accent.Render(strings.Repeat("─", max(8, m.width-4))))
	contentStart := len(lines)
	switch m.page {
	case "Print":
		m.printPage(&lines, contentStart)
	case "Printers":
		m.printerPage(&lines, contentStart)
	case "Jobs":
		m.jobsPage(&lines, contentStart)
	case "Queues":
		m.queuePage(&lines, contentStart)
	case "Help":
		m.helpPage(&lines, contentStart)
	}
	if m.loading != "" {
		lines = append(lines, "", muted.Render(m.loading+"…"))
	}
	if m.err != nil {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(safeError(m.err)))
	}
	lines = append(lines, "", muted.Render("Tab navigate · ↑ ↓ select · PgUp/PgDn scroll · Enter open/confirm · Esc back · Ctrl+C quit"))
	lines = m.viewport(lines, contentStart, 2)
	return lipgloss.NewStyle().Padding(0, 1).Width(max(1, m.width-2)).Render(strings.Join(lines, "\n"))
}

func (m *Model) accountView() string {
	lines := []string{strong.Render("◇ Print @ SoC"), accent.Render(strings.Repeat("─", max(8, m.width-4)))}
	if m.trust != nil {
		lines = append(lines, warm.Render("Verify the server before signing in"), "This is the first connection to this SoC server.", "Compare the fingerprint with SoC IT guidance before you continue.", "")
		m.add(&lines, "Server: "+safeDisplay(m.trust.Host), "", 0)
		m.add(&lines, "Fingerprint: "+safeDisplay(m.trust.Fingerprint), "", 0)
		lines = append(lines, "")
		m.add(&lines, "[ I verified it — continue ]", "trust-confirm", 0)
		m.add(&lines, "[ Cancel sign-in ]", "trust-cancel", 0)
	} else {
		m.account(&lines)
		if m.loading != "" {
			lines = append(lines, "", muted.Render(m.loading+"…"))
		}
		if m.accountNotice != "" {
			lines = append(lines, "", warm.Render(safeDisplay(m.accountNotice)))
		}
		if m.err != nil {
			lines = append(lines, "", lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(safeError(m.err)))
		}
	}
	if m.trust != nil {
		lines = append(lines, "", muted.Render("Enter or Y to continue after verification · Esc cancel · Ctrl+C quit"))
	} else {
		lines = append(lines, "", muted.Render("Enter continue · Esc back · Ctrl+C quit"))
	}
	for index := range m.zones {
		m.zones[index].y++ // account view has one row of top padding
	}
	return lipgloss.NewStyle().Padding(1, 2).Width(max(1, m.width-4)).Render(strings.Join(lines, "\n"))
}

func (m *Model) viewport(lines []string, header, footer int) []string {
	if m.height <= 0 || len(lines) <= m.height {
		m.scroll = 0
		return lines
	}
	if header+footer >= m.height {
		m.scroll = 0
		var zones []zone
		for _, area := range m.zones {
			if area.y < m.height {
				zones = append(zones, area)
			}
		}
		m.zones = zones
		return lines[:m.height]
	}
	end := len(lines) - footer
	capacity := max(1, m.height-header-footer)
	maxScroll := max(0, end-header-capacity)
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	start := header + m.scroll
	stop := min(end, start+capacity)
	visible := make([]string, 0, header+(stop-start)+footer)
	visible = append(visible, lines[:header]...)
	visible = append(visible, lines[start:stop]...)
	visible = append(visible, lines[end:]...)
	var zones []zone
	for _, area := range m.zones {
		if area.y < header {
			zones = append(zones, area)
		} else if area.y >= start && area.y < stop {
			area.y = header + area.y - start
			zones = append(zones, area)
		}
	}
	m.zones = zones
	return visible
}

func (m *Model) adjustScrollForSelection() {
	if m.height <= 0 {
		return
	}
	header := 4 + len(strings.Split(m.statusDetail, "\n"))
	if m.statusDetail == "" {
		header = 4
	}
	if m.status == "Connection unavailable" {
		header++
	}
	capacity := max(1, m.height-header-2)
	row := -1
	switch m.page {
	case "Printers":
		row = 3 + m.selected
	case "Print":
		if m.filePath != "" && m.printer.ID == "" {
			row = 2 + m.selected
		}
	case "Queues":
		if m.printer.ID == "" {
			row = 2 + m.selected
		} else if m.queue == "" {
			row = 2 + m.selected
		}
	case "Jobs":
		row = 2
		for index := 0; index < m.jobIndex && index < len(m.jobs); index++ {
			row++
			if m.jobs[index].SpoolerID != "" {
				row++
			}
		}
	}
	if row >= 0 {
		m.scroll = max(0, row-capacity+1)
	}
}

func (m *Model) selectionCount() int {
	switch m.page {
	case "Printers":
		if m.printer.ID != "" {
			return 0
		}
		return len(catalog.Filter(m.printers, m.search, m.allPrinters))
	case "Print":
		if m.editField == "filename" || m.editField == "result" || m.filePath == "" {
			return 0
		}
		if m.printer.ID == "" {
			return len(catalog.Filter(m.printers, m.search, m.allPrinters))
		}
		if m.queue == "" {
			return len(m.printer.Queues)
		}
		return 0
	case "Jobs":
		return len(m.jobs)
	case "Queues":
		if m.queueView != nil {
			return 0
		}
		if m.printer.ID == "" {
			return len(catalog.Filter(m.printers, m.search, true))
		}
		return len(m.printer.Queues)
	default:
		return 0
	}
}

func (m *Model) accountLabel() string {
	if m.settings.Username == "" {
		return "Not set"
	}
	return safeDisplay(m.settings.Username)
}

const (
	accountUsername = iota
	accountPassword
	accountKeySetup
	accountKeyUnlock
	accountEnrollKey
	accountSignedIn
	accountBlocked
)

func (m *Model) beginAccount() {
	m.page = "Account"
	m.err = nil
	m.accountNotice = ""
	m.publicKey = ""
	if m.connected {
		m.accountStep = accountSignedIn
		m.editField = ""
		m.input = nil
		return
	}
	m.accountStep = accountUsername
	m.editField = "username"
	m.input = []rune(m.settings.Username)
}

func (m *Model) submitLogin() (tea.Model, tea.Cmd) {
	if m.settings.Username == "" {
		m.accountStep = accountUsername
		m.editField = "username"
		m.input = nil
		m.accountNotice = "Enter your SoC username to continue."
		return m, nil
	}
	if m.password == "" {
		m.password = string(m.input)
	}
	if m.password == "" {
		m.accountNotice = "Enter your SoC password to continue."
		return m, nil
	}
	m.input = nil
	m.loading = "Signing in to stu.comp.nus.edu.sg"
	m.loginPending = true
	m.accountNotice = ""
	m.err = nil
	return m, m.loginCmd(m.settings.Username, m.password, m.settings.KeyPath, true)
}

func (m *Model) continueTrust() tea.Cmd {
	if m.client == nil || m.trust == nil {
		m.err = errors.New("the connection is no longer available; try signing in again")
		m.trust = nil
		return nil
	}
	if err := m.client.Trust(m.trust); err != nil {
		m.err = fmt.Errorf("could not save this server identity: %w", err)
		return nil
	}
	m.trust = nil
	_ = m.client.Close()
	m.client = nil
	m.loading = "Connecting"
	m.loginPending = true
	return m.loginCmd(m.settings.Username, m.password, m.settings.KeyPath, true)
}

func (m *Model) cancelTrust() {
	if m.client != nil {
		_ = m.client.Close()
		m.client = nil
	}
	m.trust = nil
	m.loading = ""
	m.loginPending = false
	m.password = ""
	m.accountStep = accountPassword
	m.editField = "password"
	m.input = nil
	m.accountNotice = "Sign-in paused. Enter your password to try again."
	m.err = nil
}

func (m *Model) accountBack() {
	if m.loading != "" || m.loginPending {
		return
	}
	switch m.accountStep {
	case accountPassword:
		m.password = ""
		m.accountStep = accountUsername
		m.editField = "username"
		m.input = []rune(m.settings.Username)
		m.accountNotice = ""
	case accountKeySetup:
		if m.editField == "keyconfirm" {
			m.keyPass = ""
			m.keyPhase = 1
			m.editField = "keypass"
			m.input = nil
		} else if m.editField != "" {
			m.editField = ""
			m.input = nil
		} else {
			m.accountStep = accountPassword
			m.editField = "password"
			m.input = nil
		}
	case accountKeyUnlock, accountEnrollKey:
		m.accountStep = accountKeySetup
		m.editField = ""
		m.input = nil
	case accountUsername, accountSignedIn, accountBlocked:
		m.page = "Print"
		m.err = nil
		if m.filePath == "" {
			m.editField = "filename"
			m.input = nil
		} else {
			m.editField = ""
			m.input = nil
		}
	}
}

func (m *Model) handleLoginError(err error) {
	m.loginPending = false
	m.loading = ""
	m.password = ""
	m.input = nil
	m.err = nil
	m.page = "Account"
	m.accountNotice = ""
	switch {
	case errors.Is(err, transport.ErrChangedHostKey):
		m.connected = false
		m.status = "Connection unavailable"
		m.statusDetail = ""
		m.accountStep = accountBlocked
		m.editField = ""
	case errors.Is(err, transport.ErrNoKey):
		m.status = "Connected"
		m.accountStep = accountKeySetup
		m.editField = ""
	case errors.Is(err, transport.ErrKeyEnrollment):
		m.status = "Connected"
		if m.publicKey != "" {
			m.accountStep = accountEnrollKey
			m.accountNotice = "The jump host has not accepted this key yet. Make sure it is registered with SoC."
		} else {
			m.accountStep = accountKeySetup
			m.accountNotice = "The jump host did not accept the selected key. Check its registration or choose another key."
		}
		m.editField = ""
	case errors.Is(err, transport.ErrKeyPassphrase):
		m.status = "Connected"
		m.accountStep = accountKeyUnlock
		m.editField = "keyunlock"
		m.accountNotice = "That key could not be unlocked. Try its passphrase again."
	case errors.Is(err, transport.ErrAuthentication):
		m.status = "Connected"
		m.accountStep = accountPassword
		m.editField = "password"
		m.accountNotice = "The username or password was not accepted. Check them and try again."
	case errors.Is(err, transport.ErrNetwork):
		m.status = "Connection unavailable"
		m.statusDetail = "If you're outside NUS, connect to NUS VPN and try again."
		m.accountStep = accountPassword
		m.editField = "password"
		m.accountNotice = "SoC could not be reached. If you're outside NUS, connect to NUS VPN, then try again."
	default:
		m.status = "Connection unavailable"
		m.accountStep = accountPassword
		m.editField = "password"
		m.accountNotice = "Sign-in did not complete. Check your account settings and try again."
		m.err = err
	}
}
func (m *Model) add(lines *[]string, text string, action string, index int) {
	y := len(*lines)
	*lines = append(*lines, text)
	if action != "" {
		m.zones = append(m.zones, zone{y: y, left: 0, right: m.width, action: action, index: index})
	}
}
func (m *Model) account(lines *[]string) {
	switch m.accountStep {
	case accountUsername:
		*lines = append(*lines, warm.Render("Sign in · Step 1 of 2"), "Account: "+m.accountLabel(), "Enter your SoC username.", "")
		m.inputLine(lines, "Username", string(m.input), false, "username", 0)
		*lines = append(*lines, "")
		*lines = append(*lines, terminalLink("Create a SoC account", "https://mysoc.nus.edu.sg/~newacct/"), "  "+terminalLink("Enable Unix access", "https://mysoc.nus.edu.sg/~myacct/services.cgi"))
	case accountPassword:
		*lines = append(*lines, warm.Render("Sign in · Step 2 of 2"), "SoC account: "+safeDisplay(m.settings.Username), "Enter your SoC password.", "")
		m.inputLine(lines, "Password", string(m.input), true, "password", 0)
		*lines = append(*lines, "", muted.Render("Your password is saved in macOS Keychain after sign-in."))
		m.add(lines, "[ Sign in ]", "account-submit", 0)
		m.add(lines, "[ Back to username ]", "account-back", 0)
	case accountKeySetup:
		switch m.editField {
		case "keypass":
			*lines = append(*lines, warm.Render("Secure SSH key · Step 1 of 2"), "Choose a passphrase to protect your new key.", "Use at least 12 characters.", "")
			m.inputLine(lines, "New passphrase", string(m.input), true, "keypass", 0)
		case "keyconfirm":
			*lines = append(*lines, warm.Render("Secure SSH key · Step 2 of 2"), "Enter the same passphrase again.", "")
			m.inputLine(lines, "Confirm passphrase", string(m.input), true, "keyconfirm", 0)
		case "key":
			*lines = append(*lines, warm.Render("Use an existing SSH key"), "Paste the path to your private key file.", "")
			m.inputLine(lines, "Key file", string(m.input), false, "key", 0)
		default:
			*lines = append(*lines, warm.Render("SSH key needed"), "This network needs the SoC jump host, which requires a registered SSH key.", "Choose one way to continue:", "")
			m.add(lines, "[ Create a secure SSH key ]", "create-key", 0)
			m.add(lines, "[ Use an existing SSH key ]", "select-key", 0)
			if m.publicKey != "" {
				m.add(lines, "[ View the key to register ]", "show-public-key", 0)
			}
			*lines = append(*lines, "", terminalLink("SoC SSH key setup guide", "https://dochub.comp.nus.edu.sg/cf/services/network/skeys"))
		}
	case accountKeyUnlock:
		*lines = append(*lines, warm.Render("Unlock your SSH key"), "Enter the passphrase you chose when creating this key.", "")
		m.inputLine(lines, "Key passphrase", string(m.input), true, "keyunlock", 0)
	case accountEnrollKey:
		*lines = append(*lines, warm.Render("Register your SSH key"), "Add this public key in the SoC SSH Keys service.", "Then return here and continue sign-in.", "")
		*lines = append(*lines, safeDisplay(m.publicKey), "", terminalLink("Open SoC SSH key setup", "https://dochub.comp.nus.edu.sg/cf/services/network/skeys"))
		m.add(lines, "[ I added the key — continue ]", "key-enrolled", 0)
	case accountSignedIn:
		*lines = append(*lines, warm.Render("You’re signed in"), "SoC account: "+safeDisplay(m.settings.Username), "")
		m.add(lines, "[ Return to Print ]", "account-done", 0)
		m.add(lines, "[ Change account or password ]", "change-account", 0)
		m.add(lines, "[ Forget saved sign-in ]", "forget-credentials", 0)
	case accountBlocked:
		*lines = append(*lines, warm.Render("Connection stopped for your safety"), "The saved server identity has changed.", "Check with SoC IT before trying again.", "", terminalLink("Contact SoC Technical Services", "https://dochub.comp.nus.edu.sg/cf/contact"))
		m.add(lines, "[ Return to Print ]", "account-done", 0)
	}
}

func (m *Model) printPage(lines *[]string, start int) {
	*lines = append(*lines, warm.Render("Print a PDF"))
	if m.editField == "result" {
		*lines = append(*lines, accent.Render("✓ Submitted"), "File: "+safeDisplay(m.fileName), "Queue: "+m.queue, "", safeDisplay(m.statusDetail))
		m.add(lines, "[ Print another ]", "print-start", 0)
		return
	}
	if m.submissionUnknown {
		*lines = append(*lines, warm.Render("Outcome unknown"), "File: "+safeDisplay(m.fileName), "Queue: "+safeDisplay(m.queue), "Operation: "+safeDisplay(m.unknownOperation), "The server may have accepted this job. Inspect the queue before another attempt.")
		m.add(lines, "[ Inspect this printer queue ]", "inspect-unknown-queue", 0)
		m.add(lines, "[ View Jobs ]", "view-unknown-job", 0)
		m.add(lines, "[ I checked the queue — prepare another attempt ]", "prepare-after-unknown", 0)
		return
	}
	if !m.connected {
		*lines = append(*lines, "Sign in to choose a PDF and print.")
		m.add(lines, "[ Sign in ]", "account", 0)
		return
	}
	if m.editField == "filename" {
		m.inputLine(lines, "PDF path", string(m.input), false, "filename", 0)
		*lines = append(*lines, muted.Render("Paste a path, or drag the file into this terminal, then press Enter."))
		return
	}
	if m.filePath == "" {
		m.add(lines, "Choose a PDF file…", "print-start", 0)
		return
	}
	if m.printer.ID == "" {
		m.printerChoices(lines)
		return
	}
	if m.queue == "" {
		for i, q := range m.printer.Queues {
			p := "  "
			if i == m.selected {
				p = "› "
			}
			m.add(lines, p+q, "choose-queue", i)
		}
		return
	}
	m.add(lines, "Review before submitting", "", 0)
	m.add(lines, "File: "+m.fileName, "", 0)
	m.add(lines, "Location: "+m.printer.Location, "", 0)
	m.add(lines, "Printer: "+m.printer.ID+" · "+m.printer.Paper+" · "+m.printer.Kind, "", 0)
	m.add(lines, "Sides / mode: "+m.queue, "", 0)
	m.add(lines, "Queue: "+m.queue, "", 0)
	m.add(lines, "Banner: "+m.printer.Banner, "", 0)
	m.add(lines, "Account: "+m.accountLabel()+"@stu", "", 0)
	m.add(lines, "Network: "+m.route.String(), "", 0)
	m.add(lines, "", "", 0)
	m.add(lines, "[ Confirm print ]", "submit-confirm", 0)
	m.add(lines, "[ Choose another queue ]", "change-queue", 0)
	m.add(lines, "[ Cancel ]", "print-cancel", 0)
}

func (m *Model) printerChoices(lines *[]string) {
	m.visiblePrinters = catalog.Filter(m.printers, m.search, m.allPrinters || m.page == "Queues")
	if len(m.visiblePrinters) == 0 {
		m.add(lines, "No matching printers. Enter search text or clear it.", "", 0)
		return
	}
	if m.selected >= len(m.visiblePrinters) {
		m.selected = 0
	}
	searchHint := "Choose the printer location:"
	switch m.page {
	case "Queues":
		searchHint = "Choose a printer location to inspect its queues:"
	case "Print":
		searchHint = "Choose a student printer location:"
	case "Printers":
		searchHint += " Search: " + safeDisplay(m.search) + "  ·  press / to filter  ·  A toggles all locations"
	}
	*lines = append(*lines, searchHint)
	for i, p := range m.visiblePrinters {
		prefix := "  "
		if i == m.selected {
			prefix = "› "
		}
		label := p.ID + " · " + p.Paper + " · " + p.Kind
		if !catalog.IsStudentEligible(p) {
			label += " · " + p.Access
		}
		m.add(lines, prefix+label+" — "+p.Location, "choose-printer", i)
	}
}

func (m *Model) printerPage(lines *[]string, start int) {
	*lines = append(*lines, warm.Render("Printers and locations"), "Press / to search. Press A to include restricted locations; student printing remains limited to public queues.")
	if m.printer.ID != "" {
		m.add(lines, "[ Back to all locations ]", "printer-list", 0)
		m.add(lines, "Printer: "+m.printer.ID+" · "+m.printer.Model, "", 0)
		m.add(lines, "Location: "+m.printer.Location, "", 0)
		m.add(lines, "Paper: "+m.printer.Paper+" · "+m.printer.Kind+" · banner: "+m.printer.Banner, "", 0)
		m.add(lines, "Access: "+m.printer.Access, "", 0)
		*lines = append(*lines, "Available exact queues:")
		for _, queue := range m.printer.Queues {
			*lines = append(*lines, "  "+queue)
		}
		return
	}
	m.printerChoices(lines)
	_ = start
}

func (m *Model) jobsPage(lines *[]string, start int) {
	*lines = append(*lines, warm.Render("Print jobs"))
	if !m.connected {
		m.add(lines, "Sign in to load your local history and refresh queue status.", "account", 0)
		return
	}
	if m.cancelConfirm && m.jobIndex >= 0 && m.jobIndex < len(m.jobs) {
		job := m.jobs[m.jobIndex]
		*lines = append(*lines, "Cancel "+safeDisplay(job.FileName)+" (server job "+safeDisplay(job.SpoolerID)+")? The queue will be checked again first.")
		m.add(lines, "[ Confirm cancellation ]", "confirm-cancel", 0)
		m.add(lines, "[ Keep job ]", "keep-job", 0)
		return
	}
	if len(m.jobs) == 0 {
		m.jobs, _ = m.store.List(context.Background(), 100)
	}
	m.add(lines, "[ Refresh my queues ]", "refresh-jobs", 0)
	if len(m.jobs) == 0 {
		*lines = append(*lines, "No print submissions yet.")
		return
	}
	for i, job := range m.jobs {
		prefix := "  "
		if i == m.jobIndex {
			prefix = "› "
		}
		row := fmt.Sprintf("%s %s · %s · %s · %s", prefix, safeDisplay(job.FileName), job.PrinterID, job.Queue, displayState(job.State))
		m.add(lines, row, "job", i)
		if job.SpoolerID != "" {
			*lines = append(*lines, muted.Render("    Server job "+job.SpoolerID))
		}
	}
	if m.jobIndex >= 0 && m.jobIndex < len(m.jobs) {
		job := m.jobs[m.jobIndex]
		if job.State == "queued" && job.SpoolerID != "" {
			m.add(lines, "[ Cancel selected job ]", "cancel-job", 0)
		} else {
			*lines = append(*lines, muted.Render("Only a verified matching queue entry can be cancelled."))
		}
	}
	_ = start
}

func displayState(state string) string {
	switch state {
	case "queued":
		return "Queued"
	case "gone":
		return "No longer in queue"
	case "failed":
		return "Failed"
	case "unknown":
		return "Outcome unknown"
	case "pending":
		return "Submitting"
	case "cancelled":
		return "Cancelled"
	default:
		return "Submitted"
	}
}

func (m *Model) queuePage(lines *[]string, start int) {
	*lines = append(*lines, warm.Render("Printer queues"))
	if !m.connected {
		m.add(lines, "Sign in to check live queues.", "account", 0)
		return
	}
	if m.printer.ID == "" {
		m.printerChoices(lines)
		return
	}
	if m.queue == "" {
		m.add(lines, "Printer: "+m.printer.ID+" · "+m.printer.Location, "", 0)
		m.add(lines, "[ Choose another printer ]", "queue-back", 0)
		*lines = append(*lines, "Choose a queue:")
		for i, q := range m.printer.Queues {
			prefix := "  "
			if i == m.selected {
				prefix = "› "
			}
			m.add(lines, prefix+q, "queue-choice", i)
		}
		return
	}
	if m.queueView == nil {
		m.add(lines, "[ Refresh queue ]", "refresh-queue", 0)
		return
	}
	*lines = append(*lines, "Printer: "+m.printer.ID+" · "+m.printer.Location, "Queue: "+m.queueView.Queue, "Checked: "+m.queueView.CheckedAt.Local().Format(time.RFC3339))
	m.add(lines, "[ Refresh ]", "refresh-queue", 0)
	m.add(lines, "[ Choose printer / queue ]", "queue-back", 0)
	if m.queueView.KnownEmpty {
		*lines = append(*lines, "No entries in this queue.")
	}
	if len(m.queueView.Jobs) == 0 && !m.queueView.KnownEmpty {
		*lines = append(*lines, "Queue output was not recognized. Expand details below.")
	}
	for _, job := range m.queueView.Jobs {
		label := fmt.Sprintf("%s  %s  %s  %s", job.Rank, job.Owner, job.ID, job.FileName)
		ownedIndex := -1
		for i, recorded := range m.jobs {
			if recorded.Username == m.settings.Username && recorded.Queue == m.queueView.Queue && recorded.State == "queued" && recorded.SpoolerID == job.ID && job.Owner == recorded.Username && strings.Contains(job.Raw, "document-"+recorded.ID+".pdf") {
				ownedIndex = i
				break
			}
		}
		if ownedIndex >= 0 {
			m.add(lines, label+"  [ Cancel ]", "queue-cancel", ownedIndex)
		} else {
			*lines = append(*lines, label)
		}
	}
	*lines = append(*lines, "", "Queue response:")
	for _, line := range strings.Split(m.queueView.Output, "\n") {
		*lines = append(*lines, "  "+line)
	}
	_ = start
}

func (m *Model) helpPage(lines *[]string, start int) {
	*lines = append(*lines, "Print — choose a PDF, printer location and queue, review, then confirm.", "Printers — browse the SoC printer catalog and locations.", "Jobs — saved submissions and current queue status.", "Queues — read live queue output for a selected printer.", "Network — app tests direct SSH, then the SoC jump host.", "Last reachable route: "+m.route.String(), "Outside NUS: connect NUS VPN before retrying.", terminalLink("Jump setup", "https://dochub.comp.nus.edu.sg/cf/guides/sjump/start"), terminalLink("SSH keys", "https://dochub.comp.nus.edu.sg/cf/services/network/skeys"), "Password is kept in the OS credential vault; file contents are not retained.", "Queue presence does not confirm that paper physically printed.")
	_ = start
}

func (m *Model) inputLine(lines *[]string, label, value string, secret bool, action string, index int) {
	if secret {
		value = m.mask(value)
	} else {
		value = safeDisplay(value)
	}
	if m.editField == action {
		value += "▏"
	}
	m.add(lines, label+": "+value, "", index)
}

func safeDisplay(value string) string {
	return strings.Map(func(r rune) rune {
		if r == 0 || r == 0x1b || r < 0x20 || r >= 0x7f && r <= 0x9f {
			return -1
		}
		return r
	}, strings.ToValidUTF8(value, "�"))
}

func (m *Model) mask(value string) string { return strings.Repeat("•", len([]rune(value))) }

func terminalLink(label, url string) string {
	return "\x1b]8;;" + url + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func (m *Model) click(x, y int) {
	for _, area := range m.zones {
		if area.y == y && x >= area.left && x <= area.right {
			switch area.action {
			case "tab":
				m.openTab(area.index)
			case "account":
				m.beginAccount()
			case "trust-confirm":
				m.pendingCmd = m.continueTrust()
			case "trust-cancel":
				m.cancelTrust()
			case "account-submit":
				m.password = string(m.input)
				_, m.pendingCmd = m.submitLogin()
			case "account-back":
				m.accountStep = accountUsername
				m.editField = "username"
				m.input = []rune(m.settings.Username)
				m.accountNotice = ""
			case "create-key":
				m.accountStep = accountKeySetup
				m.keyPhase = 1
				m.editField = "keypass"
				m.input = nil
				m.err = nil
			case "select-key":
				m.accountStep = accountKeySetup
				m.editField = "key"
				m.input = []rune(m.settings.KeyPath)
				m.err = nil
			case "show-public-key":
				m.accountStep = accountEnrollKey
				m.editField = ""
				m.input = nil
			case "key-enrolled":
				m.accountStep = accountPassword
				m.editField = "password"
				m.input = nil
				m.accountNotice = "Now sign in again with your SoC password."
			case "account-done":
				m.page = "Print"
				m.err = nil
			case "retry":
				m.loading = "Checking connection"
				m.pendingCmd = m.detectCmd()
			case "change-account":
				m.disconnect()
				m.beginAccount()
				m.err = nil
			case "forget-credentials":
				if m.settings.Username == "" {
					m.err = errors.New("no saved account is configured")
				} else {
					passwordErr := credentials.DeletePassword(m.settings.Username)
					passphraseErr := credentials.DeletePassphrase(m.settings.Username)
					if err := errors.Join(passwordErr, passphraseErr); err != nil {
						m.err = fmt.Errorf("could not remove all saved credentials from the OS credential vault: %w", err)
					} else {
						m.accountNotice = "Saved sign-in credentials removed from macOS Keychain."
						m.err = nil
					}
				}
			case "job":
				m.jobIndex = area.index
				m.selected = area.index
			case "cancel-job":
				m.jobIndex = area.index
				m.cancelConfirm = true
				m.err = nil
			case "queue-cancel":
				m.jobIndex = area.index
				m.cancelConfirm = true
				m.page = "Jobs"
				m.selectedTab = 2
				m.err = nil
			case "confirm-cancel":
				m.cancelSelectedJob()
			case "keep-job":
				m.cancelConfirm = false
			case "change-queue":
				m.queue = ""
				m.selected = 0
			case "inspect-unknown-queue":
				m.page = "Queues"
				m.selectedTab = 3
				m.queueView = nil
				if m.printer.ID == "" {
					if printer, ok := catalog.FindQueue(m.printers, m.queue); ok {
						m.printer = printer
					}
				}
				m.loadQueue()
			case "view-unknown-job":
				m.openTab(2)
			case "prepare-after-unknown":
				m.submissionUnknown = false
				m.unknownOperation = ""
				m.err = nil
				m.statusDetail = "You confirmed that you inspected the queue. Review and confirm any new print attempt."
			case "print-cancel":
				m.page = "Print"
				m.selectedTab = 0
				m.editField = "filename"
				m.input = nil
				m.filePath = ""
				m.fileName = ""
				m.printer = catalog.Printer{}
				m.queue = ""
			case "print-start":
				m.page = "Print"
				m.editField = "filename"
				m.input = nil
				m.printer = catalog.Printer{}
				m.queue = ""
			case "submit-confirm":
				m.confirmSubmit()
			case "refresh-queue":
				m.loadQueue()
			case "queue-back":
				m.queue = ""
				m.queueView = nil
				m.printer = catalog.Printer{}
				m.selected = 0
			case "printer-list":
				m.printer = catalog.Printer{}
				m.selected = 0
			case "refresh-jobs":
				m.refreshJobs()
			case "choose-printer":
				m.selectPrinter(area.index)
			case "choose-queue":
				m.selectQueue(area.index)
			case "queue-choice":
				m.selectQueue(area.index)
			}
			return
		}
	}
}

func (m *Model) openTab(index int) {
	if index < 0 || index >= len(tabs) {
		return
	}
	m.selectedTab = index
	m.selected = 0
	m.scroll = 0
	m.err = nil
	m.editField = ""
	m.input = nil
	switch tabs[index] {
	case "Print":
		m.page = "Print"
		if m.printer.ID != "" && !catalog.IsStudentEligible(m.printer) {
			m.printer = catalog.Printer{}
			m.queue = ""
		}
		if m.filePath == "" {
			m.editField = "filename"
		}
	case "Printers":
		m.page = "Printers"
		m.search = ""
		m.allPrinters = false
		m.printer = catalog.Printer{}
		m.queue = ""
	case "Jobs":
		m.page = "Jobs"
		m.jobIndex = 0
		if m.store != nil {
			m.jobs, _ = m.store.List(context.Background(), 100)
		}
		m.cancelConfirm = false
		if m.connected {
			m.refreshJobs()
		}
	case "Queues":
		m.page = "Queues"
		m.printer = catalog.Printer{}
		m.queue = ""
		m.queueView = nil
		m.search = ""
		if m.store != nil {
			m.jobs, _ = m.store.List(context.Background(), 100)
		}
	case "Help":
		m.page = "Help"
	}
}

func (m *Model) key(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.trust != nil {
		if key.String() == "y" || key.String() == "Y" || key.Type == tea.KeyEnter {
			return m, m.continueTrust()
		}
		if key.Type == tea.KeyEsc {
			m.cancelTrust()
			return m, nil
		}
		return m, nil
	}
	if m.loading != "" || m.loginPending {
		return m, nil
	}
	if m.page == "Account" {
		switch key.Type {
		case tea.KeyEsc:
			m.accountBack()
			return m, nil
		case tea.KeyEnter:
			return m.enter()
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
			return m, nil
		case tea.KeyRunes:
			if m.editField != "" {
				m.input = append(m.input, key.Runes...)
			}
			return m, nil
		default:
			return m, nil
		}
	}
	if key.Type == tea.KeyTab {
		m.openTab((m.selectedTab + 1) % len(tabs))
		return m, nil
	}
	if key.Type == tea.KeyShiftTab {
		m.openTab((m.selectedTab + len(tabs) - 1) % len(tabs))
		return m, nil
	}
	if key.Type == tea.KeyLeft && m.editField == "" {
		m.openTab((m.selectedTab + len(tabs) - 1) % len(tabs))
		return m, nil
	}
	if key.Type == tea.KeyRight && m.editField == "" {
		m.openTab((m.selectedTab + 1) % len(tabs))
		return m, nil
	}
	if key.Type == tea.KeyEsc {
		if m.cancelConfirm {
			m.cancelConfirm = false
			m.err = nil
			return m, nil
		}
		if m.editField != "" {
			m.editField = ""
			m.input = nil
			return m, nil
		}
		m.openTab(0)
		return m, nil
	}
	if key.Type == tea.KeyUp {
		if m.selected > 0 {
			m.selected--
		}
		if m.page == "Jobs" && m.jobIndex > 0 {
			m.jobIndex--
		}
		m.adjustScrollForSelection()
		return m, nil
	}
	if key.Type == tea.KeyDown {
		if m.selected+1 < m.selectionCount() {
			m.selected++
		}
		if m.page == "Jobs" && m.jobIndex+1 < len(m.jobs) {
			m.jobIndex++
		}
		m.adjustScrollForSelection()
		return m, nil
	}
	if key.Type == tea.KeyPgUp {
		m.scroll -= max(1, m.height/2)
		return m, nil
	}
	if key.Type == tea.KeyPgDown {
		m.scroll += max(1, m.height/2)
		return m, nil
	}
	if key.Type == tea.KeyBackspace || key.Type == tea.KeyDelete {
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		if m.page == "Printers" || m.page == "Queues" && m.printer.ID == "" {
			m.search = string(m.input)
			m.selected = 0
		}
		return m, nil
	}
	if key.Type == tea.KeyEnter {
		if m.page == "Jobs" && m.cancelConfirm {
			m.cancelSelectedJob()
			return m, m.takeCommand()
		}
		return m.enter()
	}
	if key.Type == tea.KeyRunes {
		text := string(key.Runes)
		if (m.page == "Printers" || m.page == "Queues" && m.printer.ID == "") && m.editField == "" {
			if text == "/" {
				m.editField = "search"
				m.input = []rune(m.search)
				return m, nil
			}
			if m.page == "Printers" && strings.EqualFold(text, "a") {
				m.allPrinters = !m.allPrinters
				m.selected = 0
				return m, nil
			}
		}
		if m.editField != "" {
			m.input = append(m.input, key.Runes...)
			if m.editField == "search" {
				m.search = string(m.input)
				m.selected = 0
			}
			return m, nil
		}
		if text == "r" && m.status == "Connection unavailable" {
			m.loading = "Checking"
			return m, m.detectCmd()
		}
	}
	return m, nil
}

func (m *Model) enter() (tea.Model, tea.Cmd) {
	if m.editField != "" {
		value := string(m.input)
		switch m.editField {
		case "username":
			if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`).MatchString(value) {
				m.err = errors.New("enter a valid SoC Unix username")
				return m, nil
			}
			m.settings.Username = value
			m.accountStep = accountPassword
			m.editField = "password"
			m.input = nil
			m.accountNotice = ""
			m.err = nil
		case "password":
			if m.settings.Username == "" {
				m.err = errors.New("enter your SoC username first")
				m.editField = "username"
				m.accountStep = accountUsername
				m.input = nil
				return m, nil
			}
			if value == "" {
				m.err = errors.New("enter your SoC password")
				return m, nil
			}
			m.password = value
			return m.submitLogin()
		case "key":
			path, err := expandPath(strings.Trim(strings.TrimSpace(value), "'\""))
			if err != nil {
				m.err = errors.New("enter the path to your private SSH key file")
				return m, nil
			}
			if _, err := os.Stat(path); err != nil {
				m.err = errors.New("that SSH key file could not be opened")
				return m, nil
			}
			m.settings.KeyPath = path
			if err := config.Save(m.settings); err != nil {
				m.err = fmt.Errorf("could not save the SSH key setting: %w", err)
				return m, nil
			}
			m.accountStep = accountPassword
			m.editField = "password"
			m.input = nil
			m.err = nil
		case "keyunlock":
			if value == "" {
				m.err = errors.New("enter the selected SSH key passphrase")
				return m, nil
			}
			m.keyPass = value
			m.accountStep = accountPassword
			m.editField = "password"
			m.input = nil
			m.err = nil
		case "keypass":
			if len(value) < 12 {
				m.err = errors.New("use a passphrase with at least 12 characters")
				return m, nil
			}
			m.keyPass = value
			m.keyPhase = 2
			m.editField = "keyconfirm"
			m.input = nil
		case "keyconfirm":
			if value != m.keyPass {
				m.err = errors.New("the passphrases do not match")
				m.editField = "keypass"
				m.keyPhase = 1
				m.keyPass = ""
				m.input = nil
				return m, nil
			}
			m.loading = "Creating encrypted key"
			m.input = nil
			return m, m.generateKeyCmd(value)
		case "filename":
			path := strings.Trim(strings.TrimSpace(value), "'\"")
			if path == "" {
				m.err = errors.New("enter a PDF file path")
				return m, nil
			}
			path, err := expandPath(path)
			if err != nil {
				m.err = err
				return m, nil
			}
			file, err := os.Open(path)
			if err != nil {
				m.err = fmt.Errorf("could not open the selected file: %w", err)
				return m, nil
			}
			stat, err := file.Stat()
			if err != nil {
				_ = file.Close()
				m.err = err
				return m, nil
			}
			if stat.IsDir() || stat.Size() < 5 || stat.Size() > 1_000_000_000 {
				_ = file.Close()
				m.err = errors.New("choose a nonempty PDF no larger than 1 GB")
				return m, nil
			}
			header := make([]byte, 5)
			_, err = file.Read(header)
			_ = file.Close()
			if err != nil || string(header) != "%PDF-" {
				m.err = errors.New("the selected file does not have a PDF signature")
				return m, nil
			}
			m.filePath = path
			m.fileName = filepath.Base(path)
			m.editField = ""
			m.input = nil
			m.printer = catalog.Printer{}
			m.queue = ""
			m.selected = 0
		case "search":
			m.search = value
			m.editField = ""
			m.input = nil
			m.selected = 0
		}
		return m, nil
	}
	switch m.page {
	case "Account":
		switch m.accountStep {
		case accountKeySetup:
			if m.publicKey != "" {
				m.accountStep = accountEnrollKey
				m.editField = ""
				m.input = nil
			} else {
				m.keyPhase = 1
				m.editField = "keypass"
				m.input = nil
			}
		case accountEnrollKey:
			m.accountStep = accountPassword
			m.editField = "password"
			m.input = nil
		case accountSignedIn, accountBlocked:
			m.page = "Print"
		default:
			return m, nil
		}
	case "Print":
		if m.editField == "result" {
			m.editField = "filename"
			m.filePath = ""
			m.fileName = ""
			m.printer = catalog.Printer{}
			m.queue = ""
			m.input = nil
			return m, nil
		}
		if m.filePath == "" {
			m.editField = "filename"
			m.input = nil
			return m, nil
		}
		if m.printer.ID == "" {
			m.selectPrinter(m.selected)
			return m, nil
		}
		if m.queue == "" {
			m.selectQueue(m.selected)
			return m, nil
		}
		m.confirmSubmit()
	case "Printers":
		if m.printer.ID != "" {
			m.printer = catalog.Printer{}
			m.selected = 0
		} else {
			m.selectPrinter(m.selected)
		}
	case "Jobs":
		if m.jobIndex >= 0 && m.jobIndex < len(m.jobs) && m.jobs[m.jobIndex].State == "queued" && m.jobs[m.jobIndex].SpoolerID != "" {
			m.cancelConfirm = true
		} else {
			m.err = errors.New("only a verified queue entry can be cancelled")
		}
	case "Queues":
		if m.queueView != nil {
			m.loadQueue()
		} else if m.printer.ID == "" {
			m.selectPrinter(m.selected)
		} else {
			m.selectQueue(m.selected)
		}
	}
	return m, nil
}

func (m *Model) selectPrinter(index int) {
	list := catalog.Filter(m.printers, m.search, m.allPrinters || m.page == "Queues")
	if index < 0 || index >= len(list) {
		return
	}
	printer := list[index]
	if m.page == "Print" && !catalog.IsStudentEligible(printer) {
		m.err = fmt.Errorf("%s is restricted to %s", printer.ID, printer.Access)
		return
	}
	m.printer = printer
	m.selected = 0
	if m.page == "Queues" {
		m.queue = ""
		m.queueView = nil
	} else if m.page == "Printers" {
		m.queue = ""
	} else {
		m.page = "Print"
		m.editField = ""
		m.queue = ""
	}
}

func (m *Model) selectQueue(index int) {
	if index < 0 || index >= len(m.printer.Queues) {
		return
	}
	m.queue = m.printer.Queues[index]
	m.selected = 0
	if m.page == "Queues" {
		m.loadQueue()
	}
}

func (m *Model) confirmSubmit() {
	if !m.connected || m.client == nil {
		m.err = errors.New("sign in before printing")
		return
	}
	if err := catalog.ValidateQueue(m.printers, m.printer.ID, m.queue, true); err != nil {
		m.err = err
		return
	}
	id, err := randomOperationID()
	if err != nil {
		m.err = err
		return
	}
	job := store.Job{ID: id, Username: m.settings.Username, Host: transport.UnixHost, FileName: m.fileName, PrinterID: m.printer.ID, Queue: m.queue, SubmittedAt: time.Now().UTC(), State: "pending"}
	if err := m.store.AddPending(context.Background(), job); err != nil {
		m.err = fmt.Errorf("could not record the operation locally; no print was submitted: %w", err)
		return
	}
	request := transport.PrintRequest{OperationID: id, FilePath: m.filePath, FileName: m.fileName, PrinterID: m.printer.ID, Queue: m.queue}
	m.loading = "Uploading and submitting"
	m.err = nil
	m.submissionUnknown = false
	m.unknownOperation = ""
	m.pendingCmd = m.submitCmd(request)
}

func (m *Model) loadQueue() {
	if m.client == nil {
		m.err = errors.New("sign in to inspect live printer queues")
		return
	}
	if m.queue == "" {
		if m.printer.ID == "" {
			m.err = errors.New("choose a printer and queue")
			return
		}
		m.queue = m.printer.Queues[0]
	}
	m.loading = "Checking queue"
	m.err = nil
	m.pendingCmd = m.queryCmd(m.queue)
}
func (m *Model) refreshJobs() {
	if m.client == nil {
		return
	}
	m.loading = "Refreshing job queues"
	m.pendingCmd = m.jobsRefreshCmd()
}
func (m *Model) cancelSelectedJob() {
	if m.jobIndex < 0 || m.jobIndex >= len(m.jobs) {
		return
	}
	job := m.jobs[m.jobIndex]
	if !m.cancelConfirm || job.State != "queued" || job.SpoolerID == "" {
		m.err = errors.New("this job has no verified server job ID, so it cannot be safely cancelled")
		return
	}
	m.loading = "Verifying ownership and cancelling"
	m.err = nil
	m.pendingCmd = m.cancelJobCmd(job)
}

func (m *Model) generateKeyCmd(passphrase string) tea.Cmd {
	username := m.settings.Username
	return func() tea.Msg {
		if username == "" {
			return asyncMsg{kind: "key", err: errors.New("set the SoC username before creating a key")}
		}
		public, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return asyncMsg{kind: "key", err: err}
		}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "Print @ SoC "+username, []byte(passphrase))
		if err != nil {
			return asyncMsg{kind: "key", err: err}
		}
		dir, err := config.Directory()
		if err != nil {
			return asyncMsg{kind: "key", err: err}
		}
		path := filepath.Join(dir, username+"_ed25519")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return asyncMsg{kind: "key", err: fmt.Errorf("could not create the private key file: %w", err)}
		}
		if _, err = file.Write(pem.EncodeToMemory(block)); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return asyncMsg{kind: "key", err: err}
		}
		if err = file.Close(); err != nil {
			_ = os.Remove(path)
			return asyncMsg{kind: "key", err: err}
		}
		vaultErr := credentials.SetPassphrase(username, passphrase)
		sshPublic, _ := ssh.NewPublicKey(public)
		return asyncMsg{kind: "key", value: keyResult{path: path, public: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic))), vaultErr: vaultErr}}
	}
}

func (m *Model) cancelJobCmd(job store.Job) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		err := client.Cancel(ctx, job.Queue, job.SpoolerID, job.ID)
		if err != nil {
			return asyncMsg{kind: "cancel", err: err}
		}
		return asyncMsg{kind: "cancel", value: job.ID}
	}
}

func expandPath(value string) (string, error) {
	if value == "~" {
		home, err := os.UserHomeDir()
		return home, err
	}
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, value[2:])
	}
	value, err := filepath.Abs(value)
	return filepath.Clean(value), err
}

func randomOperationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

func (m *Model) Close() error {
	if m.client == nil {
		return nil
	}
	return m.client.Close()
}

func (m *Model) disconnect() {
	if m.client != nil {
		_ = m.client.Close()
		m.client = nil
	}
	m.connected = false
	m.password = ""
	m.keyPass = ""
	m.statusDetail = "Sign in to the selected SoC account to print or inspect queues."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
