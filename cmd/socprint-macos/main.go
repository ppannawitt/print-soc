package main

/*
#cgo darwin CFLAGS: -fobjc-arc -fblocks -mmacosx-version-min=13.0
#cgo darwin LDFLAGS: -framework Cocoa -framework WebKit -framework UniformTypeIdentifiers -framework LocalAuthentication -framework PDFKit
#include <stdlib.h>
void socprint_run(const char *html);
void socprint_respond(const char *response);
*/
import "C"

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"embed"

	"golang.org/x/crypto/ssh"

	"socprint/internal/catalog"
	"socprint/internal/config"
	"socprint/internal/credentials"
	"socprint/internal/store"
	"socprint/internal/transport"
)

//go:embed ui/*
var uiAssets embed.FS

type uiRequest struct {
	PrintSettings json.RawMessage
	ID            string
	Action        string
	Username      string
	Password      string
	KeyPath       string
	KeyPassphrase string
	Passphrase    string
	FilePath      string
	PrinterID     string
	Queue         string
	FileName      string
	PageRange     string
	Copies        int
	JobID         string
	TermsVersion  string
	Forget        bool
	ClearKey      bool
}

type uiResponse struct {
	ID    string
	OK    bool
	Data  any
	Code  string
	Error string
}

type application struct {
	accountMu     sync.Mutex
	epoch         uint64
	mu            sync.Mutex
	settings      config.Settings
	printers      []catalog.Printer
	history       *store.Store
	client        *transport.SSH
	pendingClient *transport.SSH
	pendingTrust  *transport.TrustRequired
	username      string
	password      string
	keyPassphrase string
	route         transport.Route
}

var activeApp *application

func main() {
	runtime.LockOSThread()
	printers, err := catalog.All()
	if err != nil {
		fatal(err)
	}
	settings, err := config.Load()
	if err != nil {
		fatal(fmt.Errorf("could not read local settings: %w", err))
	}
	history, err := store.Open()
	if err != nil {
		fatal(fmt.Errorf("could not open print history: %w", err))
	}
	activeApp = &application{settings: settings, printers: printers, history: history, username: settings.Username}
	assets := make(map[string]string)
	entries, _ := uiAssets.ReadDir("ui")
	for _, entry := range entries {
		if !entry.IsDir() {
			data, _ := uiAssets.ReadFile("ui/" + entry.Name())
			assets[entry.Name()] = string(data)
		}
	}
	encodedAssets, _ := json.Marshal(assets)
	html := C.CString(string(encodedAssets))
	C.socprint_run(html)
	C.free(unsafe.Pointer(html))
	activeApp.close()
	_ = history.Close()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "Print @ SoC:", err)
	os.Exit(1)
}

//export goHandleMessage
func goHandleMessage(raw *C.char) {
	if activeApp == nil || raw == nil {
		return
	}
	var request uiRequest
	if err := json.Unmarshal([]byte(C.GoString(raw)), &request); err != nil {
		respond(uiResponse{OK: false, Code: "invalidRequest", Error: "The app could not read that request."})
		return
	}
	go activeApp.handle(request)
}

//export goRevealSecret
func goRevealSecret(rawID, rawSecret *C.char) {
	if activeApp == nil || rawID == nil || rawSecret == nil {
		return
	}
	id, secret := C.GoString(rawID), C.GoString(rawSecret)
	go func() { respond(activeApp.revealSecret(id, secret)) }()
}

//export goTermsAccepted
func goTermsAccepted() C.int {
	if activeApp == nil {
		return 0
	}
	activeApp.mu.Lock()
	defer activeApp.mu.Unlock()
	if activeApp.settings.TermsVersion == config.TermsVersion {
		return 1
	}
	return 0
}

func respond(response uiResponse) {
	encoded, err := json.Marshal(response)
	if err != nil {
		encoded = []byte("{\"OK\":false,\"Code\":\"encoding\",\"Error\":\"The app could not prepare its response.\"}")
	}
	value := C.CString(string(encoded))
	C.socprint_respond(value)
	C.free(unsafe.Pointer(value))
}

func result(id string, data any) uiResponse {
	return uiResponse{ID: id, OK: true, Data: data}
}

func failure(id, code, message string, data any) uiResponse {
	return uiResponse{ID: id, OK: false, Data: data, Code: code, Error: message}
}

func (a *application) handle(request uiRequest) {
	switch request.Action {
	case "saveCredentials", "signIn", "trustHost", "cancelTrust", "createKey", "getPublicKey", "useKey", "signOut", "shutdown":
		a.accountMu.Lock()
		defer a.accountMu.Unlock()
	}

	var response uiResponse
	if request.Action != "initialize" && request.Action != "acceptTerms" && request.Action != "shutdown" {
		a.mu.Lock()
		termsAccepted := a.settings.TermsVersion == config.TermsVersion
		a.mu.Unlock()
		if !termsAccepted {
			respond(failure(request.ID, "termsRequired", "Review and accept the Disclaimer and Terms of Use before using Print @ SoC.", nil))
			return
		}
	}
	switch request.Action {
	case "initialize":
		response = a.initialize(request.ID)
	case "acceptTerms":
		response = a.acceptTerms(request.ID, request.TermsVersion)
	case "detectNetwork":
		response = a.detectNetwork(request.ID)
	case "saveCredentials":
		response = a.saveCredentials(request)
	case "signIn":
		response = a.signIn(request.ID, request.Username, request.Password, request.KeyPath, request.KeyPassphrase)
	case "trustHost":
		response = a.trustHost(request.ID)
	case "cancelTrust":
		response = a.cancelTrust(request.ID)
	case "createKey":
		response = a.createKey(request.ID, request.Passphrase)
	case "getPublicKey":
		response = a.getPublicKey(request.ID)
	case "useKey":
		response = a.useKey(request)
	case "validateFile":
		response = a.validateFile(request.ID, request.FilePath)
	case "submitPrint":
		response = a.submitPrint(request)
	case "listJobs", "refreshJobs":
		response = a.listJobs(request.ID, true)
	case "queue":
		response = a.queue(request.ID, request.Queue)
	case "cancelJob":
		response = a.cancelJob(request.ID, request.JobID)
	case "signOut":
		response = a.signOut(request.ID, request.Forget)
	case "shutdown":
		a.close()
		response = result(request.ID, map[string]any{"Closed": true})
	default:
		response = failure(request.ID, "unknownAction", "That action is not available.", nil)
	}
	respond(response)
}

func (a *application) acceptTerms(id, version string) uiResponse {
	if version != config.TermsVersion {
		return failure(id, "termsVersion", "These terms have changed. Review and accept the current version to continue.", nil)
	}
	a.mu.Lock()
	settings := a.settings
	a.mu.Unlock()
	settings.TermsVersion = config.TermsVersion
	if err := config.Save(settings); err != nil {
		return failure(id, "termsSaveFailed", "Print @ SoC could not save your acceptance on this Mac.", nil)
	}
	a.mu.Lock()
	a.settings = settings
	a.mu.Unlock()
	return result(id, map[string]any{"Accepted": true, "TermsVersion": config.TermsVersion})
}

func (a *application) initialize(id string) uiResponse {
	a.mu.Lock()
	username, keyPath := a.settings.Username, a.settings.KeyPath
	acceptedTermsVersion := a.settings.TermsVersion
	printers := a.printers
	a.mu.Unlock()
	savedPassword, savedPassphrase := false, false
	if username != "" {
		password, err := credentials.GetPassword(username)
		savedPassword = err == nil && password != ""
		passphrase, err := credentials.GetPassphrase(username)
		savedPassphrase = err == nil && passphrase != ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	recentJobs, _ := a.history.List(ctx, 8)
	printerRows := make([]map[string]any, 0, len(printers))
	for _, printer := range printers {
		// Keep the UI contract explicit: the catalog JSON is intentionally
		// lower-case, while the rest of the native bridge returns title-case keys.
		printerRows = append(printerRows, map[string]any{
			"ID": printer.ID, "Location": printer.Location, "Model": printer.Model,
			"Banner": printer.Banner, "Kind": printer.Kind, "Paper": printer.Paper,
			"Access": printer.Access, "Queues": printer.Queues,
		})
	}
	jobRows := make([]map[string]any, 0, len(recentJobs))
	for _, job := range recentJobs {
		jobRows = append(jobRows, map[string]any{
			"ID": job.ID, "Username": job.Username, "Host": job.Host, "FileName": job.FileName,
			"PrinterID": job.PrinterID, "Queue": job.Queue, "SubmittedAt": job.SubmittedAt,
			"State": job.State, "SpoolerID": job.SpoolerID, "Message": job.Message, "PrintSettings": job.PrintSettings,
		})
	}
	return result(id, map[string]any{
		"Username": username, "KeyPath": keyPath,
		"HasSavedPassword": savedPassword, "HasSavedKeyPassphrase": savedPassphrase,
		"Printers": printerRows, "Jobs": jobRows, "Server": transport.UnixHost,
		"TermsVersion": config.TermsVersion, "AcceptedTermsVersion": acceptedTermsVersion,
	})
}

func (a *application) revealSecret(id, secret string) uiResponse {
	a.mu.Lock()
	username := a.settings.Username
	epoch := a.epoch
	termsAccepted := a.settings.TermsVersion == config.TermsVersion
	a.mu.Unlock()
	if !termsAccepted {
		return failure(id, "termsRequired", "Review and accept the Disclaimer and Terms of Use before viewing saved credentials.", nil)
	}
	if username == "" {
		return failure(id, "secretUnavailable", "Save a SoC account before revealing credentials.", nil)
	}
	var value string
	var err error
	switch secret {
	case "password":
		value, err = credentials.GetPassword(username)
	case "keyPassphrase":
		value, err = credentials.GetPassphrase(username)
	default:
		return failure(id, "invalidSecret", "That saved credential cannot be revealed.", nil)
	}
	if err != nil || value == "" {
		return failure(id, "secretUnavailable", "That credential is not saved in Keychain. Enter it in Account settings and save it first.", nil)
	}
	a.mu.Lock()
	sameAccount := a.epoch == epoch
	a.mu.Unlock()
	if !sameAccount {
		return failure(id, "secretUnavailable", "The account changed. Authenticate again.", nil)
	}
	return result(id, map[string]any{"Secret": secret, "Value": value})
}

func (a *application) detectNetwork(id string) uiResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	probe := transport.DetectNetwork(ctx)
	if probe.Err != nil {
		return result(id, map[string]any{"Available": false, "Message": "SoC servers are not reachable right now. Sign-in will retry automatically."})
	}
	return result(id, map[string]any{"Available": true, "Route": probe.Route.String()})
}

func (a *application) saveCredentials(request uiRequest) uiResponse {
	a.mu.Lock()
	settings := a.settings
	oldUsername := settings.Username
	oldKeyPath := settings.KeyPath
	oldPassword, oldKeyPassphrase := a.password, a.keyPassphrase
	oldClient := a.client
	a.mu.Unlock()
	username := strings.TrimSpace(request.Username)
	if username == "" || !regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$").MatchString(username) {
		return failure(request.ID, "invalidUsername", "Enter a valid SoC Unix username.", nil)
	}
	settings.Username = username
	if request.ClearKey {
		settings.KeyPath = ""
	} else if strings.TrimSpace(request.KeyPath) != "" {
		settings.KeyPath = request.KeyPath
	}
	changed := oldUsername != username || settings.KeyPath != oldKeyPath ||
		(request.Password != "" && request.Password != oldPassword) ||
		(request.KeyPassphrase != "" && request.KeyPassphrase != oldKeyPassphrase)
	if err := config.Save(settings); err != nil {
		return failure(request.ID, "settingsFailed", "Print @ SoC could not save the account settings on this Mac.", nil)
	}
	vaultWarning := ""
	passwordSaved := hasPassword(username)
	keyPassphraseSaved := hasKeyPassphrase(username)
	if request.Password != "" {
		if err := credentials.SetPassword(username, request.Password); err != nil {
			vaultWarning = "The Mac credential vault is unavailable. Your password will be used for this session only."
			passwordSaved = false
		} else {
			passwordSaved = true
		}
	}
	if request.KeyPassphrase != "" {
		if err := credentials.SetPassphrase(username, request.KeyPassphrase); err != nil {
			keyPassphraseSaved = false
			if vaultWarning == "" {
				vaultWarning = "The key passphrase could not be saved in Keychain. You can enter it again when needed."
			}
		} else {
			keyPassphraseSaved = true
		}
	}
	if oldKeyPath != settings.KeyPath && request.KeyPassphrase == "" {
		if err := credentials.DeletePassphrase(username); err != nil {
			vaultWarning = "The previous key passphrase could not be removed from Keychain. It will not be used for the selected key; clear the saved credential and retry."
		}
		keyPassphraseSaved = false
	}
	a.mu.Lock()
	a.settings = settings
	a.epoch++
	a.username = username
	if changed {
		a.client = nil
		a.route = transport.RouteUnknown
	}
	if request.Password != "" {
		a.password = request.Password
	} else if oldUsername != username {
		a.password = ""
	}
	if request.KeyPassphrase != "" {
		a.keyPassphrase = request.KeyPassphrase
	} else if oldKeyPath != settings.KeyPath || oldUsername != username {
		a.keyPassphrase = ""
	}
	a.mu.Unlock()
	if changed && oldClient != nil {
		_ = oldClient.Close()
	}
	if request.Password == "" {
		if password, err := credentials.GetPassword(username); err == nil {
			a.mu.Lock()
			a.password = password
			a.mu.Unlock()
		}
	}
	if request.KeyPassphrase == "" && oldKeyPath == settings.KeyPath {
		if passphrase, err := credentials.GetPassphrase(username); err == nil {
			a.mu.Lock()
			a.keyPassphrase = passphrase
			a.mu.Unlock()
		}
	}
	return result(request.ID, map[string]any{
		"Saved": true, "Username": username, "KeyPath": settings.KeyPath,
		"HasPassword": passwordSaved, "HasKeyPassphrase": keyPassphraseSaved,
		"VaultWarning": vaultWarning, "Disconnected": changed,
	})
}

func hasPassword(username string) bool {
	value, err := credentials.GetPassword(username)
	return err == nil && value != ""
}

func hasKeyPassphrase(username string) bool {
	value, err := credentials.GetPassphrase(username)
	return err == nil && value != ""
}

func (a *application) signIn(id, username, password, keyPath, keyPassphrase string) uiResponse {
	a.mu.Lock()
	if username == "" {
		username = a.settings.Username
	}
	if password == "" && username == a.settings.Username {
		password = a.password
	}
	if keyPath == "" && username == a.settings.Username {
		keyPath = a.settings.KeyPath
	}
	if keyPassphrase == "" && username == a.settings.Username {
		keyPassphrase = a.keyPassphrase
	}
	a.mu.Unlock()
	if password == "" {
		if stored, err := credentials.GetPassword(username); err == nil {
			password = stored
		}
	}
	if keyPassphrase == "" {
		if stored, err := credentials.GetPassphrase(username); err == nil {
			keyPassphrase = stored
		}
	}
	if username == "" {
		return failure(id, "noAccount", "Add your SoC username in Account before connecting.", nil)
	}
	if password == "" {
		return failure(id, "noPassword", "Add your SoC password in Account before connecting.", nil)
	}
	// Save credentials before testing the network or SSH key. A missing jump-host
	// key must not force the user to re-enter a password on the next attempt.
	saved := a.saveCredentials(uiRequest{ID: id, Username: username, Password: password, KeyPath: keyPath, KeyPassphrase: keyPassphrase})
	if !saved.OK {
		return saved
	}
	a.mu.Lock()
	printers := a.printers
	oldPending := a.pendingClient
	a.pendingClient, a.pendingTrust = nil, nil
	a.mu.Unlock()
	if oldPending != nil {
		_ = oldPending.Close()
	}
	client, err := transport.New(username, password, keyPath, printers, keyPassphrase)
	if err != nil {
		return failure(id, "signInFailed", "Print @ SoC could not prepare a secure connection. Review the account settings and try again.", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	route, err := client.Connect(ctx, true)
	if err != nil {
		var trust *transport.TrustRequired
		if errors.As(err, &trust) {
			a.mu.Lock()
			a.pendingClient, a.pendingTrust = client, trust
			a.password, a.keyPassphrase = password, keyPassphrase
			a.mu.Unlock()
			return result(id, map[string]any{"NeedsTrust": true, "Host": trust.Host, "Fingerprint": trust.Fingerprint})
		}
		_ = client.Close()
		code, message := loginError(err)
		return failure(id, code, message, nil)
	}
	return a.finishLogin(id, client, route, username)
}

func (a *application) trustHost(id string) uiResponse {
	a.mu.Lock()
	client, trust := a.pendingClient, a.pendingTrust
	username := a.settings.Username
	a.mu.Unlock()
	if client == nil || trust == nil {
		return failure(id, "trustExpired", "That verification request expired. Return to Account and try connecting again.", nil)
	}
	if err := client.Trust(trust); err != nil {
		return failure(id, "trustFailed", "The server identity could not be saved. Review Account and try again.", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	route, err := client.Connect(ctx, true)
	if err != nil {
		var next *transport.TrustRequired
		if errors.As(err, &next) {
			a.mu.Lock()
			a.pendingTrust = next
			a.mu.Unlock()
			return result(id, map[string]any{"NeedsTrust": true, "Host": next.Host, "Fingerprint": next.Fingerprint})
		}
		_ = client.Close()
		a.mu.Lock()
		a.pendingClient, a.pendingTrust = nil, nil
		a.mu.Unlock()
		code, message := loginError(err)
		return failure(id, code, message, nil)
	}
	return a.finishLogin(id, client, route, username)
}

func (a *application) finishLogin(id string, client *transport.SSH, route transport.Route, username string) uiResponse {
	a.mu.Lock()
	oldClient := a.client
	a.client, a.pendingClient, a.pendingTrust = client, nil, nil
	a.username, a.route = username, route
	vaultWarning := ""
	if password, err := credentials.GetPassword(username); err != nil || password == "" {
		vaultWarning = "Connected for this session. Add a password in Account if you want it saved for next time."
	}
	a.mu.Unlock()
	if oldClient != nil && oldClient != client {
		_ = oldClient.Close()
	}
	return result(id, map[string]any{
		"SignedIn": true, "Username": username, "Route": route.String(),
		"VaultWarning": vaultWarning,
	})
}

func loginError(err error) (string, string) {
	switch {
	case errors.Is(err, transport.ErrChangedHostKey):
		return "changedHostKey", "The saved identity for this server has changed. Connection stopped. Check with SoC IT before changing the trusted identity."
	case errors.Is(err, transport.ErrNoKey):
		return "noKey", "The direct server is not reachable and the SoC jump host requires a registered SSH key. This is separate from your password. Register a public key manually with SoC, then choose its private key in Account. Alternatively, connect to the SoC network and try again."
	case errors.Is(err, transport.ErrKeyEnrollment):
		return "keyEnrollment", "The jump host did not accept this SSH key. Confirm that its public key is enrolled with SoC, then update or select the key in Account."
	case errors.Is(err, transport.ErrKeyPassphrase):
		return "keyPassphrase", "This SSH key could not be unlocked. Enter the correct key passphrase or select another key in Account."
	case errors.Is(err, transport.ErrAuthentication):
		return "authentication", "SoC did not accept this username/password. Edit the credentials in Account and try again. If the account is new, check that Unix access is enabled."
	case errors.Is(err, transport.ErrNetwork):
		return "network", "SoC could not be reached. Check your internet connection. If you are outside NUS, connect to NUS VPN and retry."
	default:
		return "signInFailed", "Sign-in did not complete. Review Account, check your network, and try again."
	}
}

func (a *application) cancelTrust(id string) uiResponse {
	a.mu.Lock()
	client := a.pendingClient
	a.pendingClient, a.pendingTrust = nil, nil
	a.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
	return result(id, map[string]any{"Cancelled": true})
}

func (a *application) createKey(id, passphrase string) uiResponse {
	a.mu.Lock()
	settings, username := a.settings, a.settings.Username
	a.mu.Unlock()
	replacingSavedKey := strings.TrimSpace(settings.KeyPath) != ""
	if username == "" {
		return failure(id, "noAccount", "Save your SoC username in Account before creating a key.", nil)
	}
	if len(passphrase) < 8 {
		return failure(id, "weakPassphrase", "Choose a key passphrase with at least 8 characters.", nil)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return failure(id, "keyCreateFailed", "Print @ SoC could not generate an SSH key.", nil)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "Print @ SoC "+username, []byte(passphrase))
	if err != nil {
		return failure(id, "keyCreateFailed", "Print @ SoC could not encrypt the SSH key.", nil)
	}
	directory, err := config.Directory()
	if err != nil {
		return failure(id, "keyCreateFailed", "Print @ SoC could not create its secure settings folder.", nil)
	}
	safeUser := regexp.MustCompile("[^a-zA-Z0-9_.-]").ReplaceAllString(username, "_")
	if safeUser == "" || safeUser == "." || safeUser == ".." {
		safeUser = "account"
	}
	keyPath := filepath.Join(directory, safeUser+"_ed25519")
	file, err := os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	existingKeyPreserved := false
	if errors.Is(err, os.ErrExist) {
		// A replacement gets its own private file and becomes the active key.
		// Keep the previously selected key file intact.
		file, err = os.CreateTemp(directory, safeUser+"_ed25519-new-*")
		if err == nil {
			keyPath = file.Name()
			existingKeyPreserved = true
		}
	}
	if err != nil {
		return failure(id, "keyCreateFailed", "Print @ SoC could not create a private key file in its secure folder.", nil)
	}
	if _, err := file.Write(pem.EncodeToMemory(block)); err != nil {
		_ = file.Close()
		_ = os.Remove(keyPath)
		return failure(id, "keyCreateFailed", "Print @ SoC could not save the private key.", nil)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(keyPath)
		return failure(id, "keyCreateFailed", "Print @ SoC could not finish saving the private key.", nil)
	}
	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		_ = os.Remove(keyPath)
		return failure(id, "keyCreateFailed", "Print @ SoC could not prepare the public key.", nil)
	}
	settings.KeyPath = keyPath
	if err := config.Save(settings); err != nil {
		_ = os.Remove(keyPath)
		return failure(id, "keyCreateFailed", "Print @ SoC could not save the key location.", nil)
	}
	vaultWarning := ""
	if err := credentials.SetPassphrase(username, passphrase); err != nil {
		vaultWarning = "Keychain could not save this key passphrase. You can enter it again when needed."
	}
	a.mu.Lock()
	oldClient, oldPending := a.client, a.pendingClient
	a.client, a.pendingClient, a.pendingTrust = nil, nil, nil
	a.route = transport.RouteUnknown
	a.settings, a.keyPassphrase = settings, passphrase
	a.epoch++
	a.mu.Unlock()
	if oldClient != nil {
		_ = oldClient.Close()
	}
	if oldPending != nil {
		_ = oldPending.Close()
	}
	publicKeyLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic)))
	data := map[string]any{
		"KeyPath": keyPath, "PublicKey": publicKeyLine,
		"VaultWarning": vaultWarning, "ReplacedExistingKey": replacingSavedKey,
		"ExistingKeyPreserved": existingKeyPreserved, "Disconnected": true,
	}
	data["ManualRegistrationRequired"] = true
	return result(id, data)
}

// Public-key export is local only. Registration with SoC is performed by the user.
func (a *application) getPublicKey(id string) uiResponse {
	a.mu.Lock()
	path, username, passphrase := a.settings.KeyPath, a.settings.Username, a.keyPassphrase
	a.mu.Unlock()
	if path == "" {
		return failure(id, "noKey", "Create or choose an SSH key first.", nil)
	}
	if passphrase == "" && username != "" {
		passphrase, _ = credentials.GetPassphrase(username)
	}
	publicKey, err := publicKeyFromPrivateFile(path, passphrase)
	if err != nil {
		return failure(id, "keyPassphrase", keyUnlockError(err), nil)
	}
	return result(id, map[string]any{"PublicKey": publicKey})
}

func publicKeyFromPrivateFile(path, passphrase string) (string, error) {
	signer, err := transport.LoadPrivateKey(path, passphrase)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}

func keyUnlockError(err error) string {
	var missing *ssh.PassphraseMissingError
	switch {
	case errors.As(err, &missing):
		return "This SSH key is encrypted. Enter its key passphrase in Account, save, and retry. The key passphrase can differ from your SoC password."
	case errors.Is(err, x509.IncorrectPasswordError):
		return "The SSH key passphrase did not unlock this key. Enter the passphrase used when the key was created; this is separate from your SoC password."
	case errors.Is(err, os.ErrNotExist), errors.Is(err, os.ErrPermission):
		return "The selected private key file is unavailable or unreadable. Choose the private key file again."
	default:
		return "The selected file is not a readable, supported SSH private key. Choose the private key file, not its .pub file."
	}
}

func (a *application) useKey(request uiRequest) uiResponse {
	if strings.TrimSpace(request.KeyPath) == "" {
		return failure(request.ID, "noKey", "Choose an SSH private key file first.", nil)
	}
	a.mu.Lock()
	settings := a.settings
	settings.KeyPath = request.KeyPath
	a.settings = settings
	a.mu.Unlock()
	if err := config.Save(settings); err != nil {
		return failure(request.ID, "settingsFailed", "Print @ SoC could not save the SSH key location.", nil)
	}
	return a.signIn(request.ID, request.Username, request.Password, request.KeyPath, request.KeyPassphrase)
}

func (a *application) validateFile(id, path string) uiResponse {
	file, err := os.Open(path)
	if err != nil {
		return failure(id, "fileUnreadable", "Print @ SoC could not read that file. Choose it again.", nil)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() < 5 || stat.Size() > 1_000_000_000 {
		return failure(id, "invalidPDF", "Choose a non-empty PDF no larger than 1 GB.", nil)
	}
	var signature [5]byte
	if _, err := io.ReadFull(file, signature[:]); err != nil || string(signature[:]) != "%PDF-" {
		return failure(id, "invalidPDF", "That file does not have a PDF signature.", nil)
	}
	return result(id, map[string]any{"FilePath": path, "FileName": filepath.Base(path), "Size": stat.Size()})
}

func (a *application) submitPrint(request uiRequest) uiResponse {
	a.mu.Lock()
	client, username, route := a.client, a.username, a.route
	printers := a.printers
	a.mu.Unlock()
	if client == nil || username == "" {
		return failure(request.ID, "signInRequired", "Add your SoC account in Account, then try printing again.", nil)
	}
	if request.Username != username {
		return failure(request.ID, "accountChanged", "The account changed after this document was prepared. Review the account and confirm printing again.", nil)
	}
	if err := catalog.ValidateQueue(printers, request.PrinterID, request.Queue, true); err != nil {
		return failure(request.ID, "invalidQueue", "Choose an available student printer and queue.", nil)
	}
	var options struct {
		Paper         string
		Copies        int
		PageRange     string
		Orientation   string
		ScaleMode     string
		ScalePercent  float64
		PagesPerSheet int
		AutoRotate    bool
	}
	if json.Unmarshal(request.PrintSettings, &options) != nil {
		return failure(request.ID, "invalidOptions", "Check the print settings.", nil)
	}
	printer, _ := catalog.Find(printers, request.PrinterID)
	if options.Paper != printer.Paper || options.Copies != request.Copies || options.PageRange != request.PageRange ||
		(options.Orientation != "portrait" && options.Orientation != "landscape") || (options.ScaleMode != "fit" && options.ScaleMode != "fill" && options.ScaleMode != "percent") || options.ScalePercent < 1 || options.ScalePercent > 400 ||
		(options.PagesPerSheet != 1 && options.PagesPerSheet != 2 && options.PagesPerSheet != 4 && options.PagesPerSheet != 6 && options.PagesPerSheet != 9 && options.PagesPerSheet != 16) {
		return failure(request.ID, "invalidOptions", "The prepared document does not match the selected printer settings.", nil)
	}
	if request.Copies == 0 {
		request.Copies = 1
	}
	if request.Copies < 1 || request.Copies > 99 {
		return failure(request.ID, "invalidCopies", "Choose between 1 and 99 copies.", nil)
	}
	file, err := os.Open(request.FilePath)
	if err != nil {
		return failure(request.ID, "fileUnreadable", "The PDF is no longer available. Choose it again.", nil)
	}
	info, statErr := file.Stat()
	var signature [5]byte
	_, readErr := io.ReadFull(file, signature[:])
	_ = file.Close()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() < 5 || info.Size() > 1_000_000_000 || readErr != nil || string(signature[:]) != "%PDF-" {
		return failure(request.ID, "invalidPDF", "The selected PDF changed or is no longer valid. Choose it again.", nil)
	}
	fileName := strings.TrimSpace(filepath.Base(request.FileName))
	if fileName == "" || fileName == "." || fileName == string(filepath.Separator) {
		fileName = filepath.Base(request.FilePath)
	}
	operationID, err := newOperationID()
	if err != nil {
		return failure(request.ID, "operationFailed", "Print @ SoC could not prepare a safe print operation.", nil)
	}
	job := store.Job{
		ID: operationID, Username: username, Host: transport.UnixHost,
		FileName: fileName, PrinterID: request.PrinterID,
		Queue: request.Queue, SubmittedAt: time.Now().UTC(), State: "pending", PrintSettings: string(request.PrintSettings),
	}
	if err := a.history.AddPending(context.Background(), job); err != nil {
		return failure(request.ID, "historyFailed", "Print @ SoC could not save this operation before sending it.", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	submission, submitErr := client.Submit(ctx, transport.PrintRequest{
		OperationID: operationID, FilePath: request.FilePath, FileName: job.FileName,
		PrinterID: request.PrinterID, Queue: request.Queue, Copies: request.Copies,
	})
	if submitErr != nil {
		state := "failed"
		message := "The print command did not confirm success. Check Jobs before trying again."
		var unknown *transport.SubmissionError
		if errors.As(submitErr, &unknown) && unknown.OutcomeUnknown {
			state = "unknown"
			message = "The connection ended after submission may have started. Check this printer's queue before sending again."
		}
		warning := ""
		if strings.Contains(submission.Output, "cleanup") {
			warning = "Temporary upload cleanup could not be confirmed. Reconnect and contact SoC support if cleanup remains unavailable."
			message += " " + warning
		}
		_ = a.history.Finish(context.Background(), operationID, state, "", message)
		return result(request.ID, map[string]any{
			"State": state, "OperationID": operationID, "Queue": request.Queue,
			"FileName": job.FileName, "Copies": request.Copies, "PageRange": request.PageRange,
			"Message": message, "Route": route.String(), "Warning": warning,
		})
	}
	warning := ""
	if strings.Contains(submission.Output, "cleanup") || strings.Contains(submission.Output, "could not") {
		warning = "The server accepted this job, but cleanup or recovery could not be confirmed. Check Jobs before retrying."
	}
	if err := a.history.Finish(context.Background(), operationID, "submitted", submission.SpoolerID, submission.Output); err != nil {
		warning = "The server accepted the job, but local history could not be updated."
	}
	if submission.Route != transport.RouteUnknown {
		route = submission.Route
		a.mu.Lock()
		a.route = route
		a.mu.Unlock()
	}
	return result(request.ID, map[string]any{
		"State": "submitted", "OperationID": submission.OperationID, "Queue": request.Queue,
		"FileName": job.FileName, "Copies": request.Copies, "PageRange": request.PageRange,
		"SpoolerID": submission.SpoolerID, "Route": route.String(),
		"Message": "SoC accepted the print command. This confirms submission, not that pages have physically printed.",
		"Warning": warning,
	})
}

func newOperationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", value[:]), nil
}

func (a *application) listJobs(id string, refresh bool) uiResponse {
	a.mu.Lock()
	client, username := a.client, a.username
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	jobs, err := a.history.List(ctx, 100)
	if err != nil {
		return failure(id, "historyFailed", "Print @ SoC could not read local job history.", nil)
	}
	if refresh && client != nil && username != "" {
		queues := make(map[string]bool)
		for _, job := range jobs {
			if job.Username == username && (job.State == "submitted" || job.State == "queued") {
				queues[job.Queue] = true
			}
		}
		snapshots := make(map[string]transport.QueueSnapshot)
		for queue := range queues {
			if ctx.Err() != nil {
				break
			}
			queueCtx, queueCancel := context.WithTimeout(ctx, 18*time.Second)
			snapshot, queueErr := client.Queue(queueCtx, queue)
			queueCancel()
			if queueErr == nil {
				snapshots[queue] = snapshot
			}
		}
		for i := range jobs {
			snapshot, ok := snapshots[jobs[i].Queue]
			if ok && jobs[i].Username == username {
				a.refreshJobStatus(&jobs[i], snapshot)
			}
		}
		jobs, _ = a.history.List(ctx, 100)
	}
	rows := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		rows = append(rows, map[string]any{
			"ID": job.ID, "Username": job.Username, "Host": job.Host, "FileName": job.FileName,
			"PrinterID": job.PrinterID, "Queue": job.Queue, "SubmittedAt": job.SubmittedAt,
			"State": job.State, "SpoolerID": job.SpoolerID, "Message": job.Message, "PrintSettings": job.PrintSettings,
		})
	}
	return result(id, map[string]any{"Jobs": rows, "RefreshedAt": time.Now().UTC()})
}

func (a *application) refreshJobStatus(job *store.Job, snapshot transport.QueueSnapshot) {
	if !snapshot.KnownEmpty && len(snapshot.Jobs) == 0 {
		return
	}
	needle := "document-" + job.ID + ".pdf"
	for _, entry := range snapshot.Jobs {
		if entry.Owner == job.Username && strings.Contains(entry.Raw, needle) {
			_ = a.history.Finish(context.Background(), job.ID, "queued", entry.ID, "The matching job is in the current server queue.")
			job.State, job.SpoolerID = "queued", entry.ID
			return
		}
	}
	if snapshot.KnownEmpty || snapshot.Complete {
		_ = a.history.Finish(context.Background(), job.ID, "gone", job.SpoolerID, "The submission is no longer listed in the current queue.")
		job.State = "gone"
	}
}

func (a *application) queue(id, queue string) uiResponse {
	a.mu.Lock()
	client, printers := a.client, a.printers
	a.mu.Unlock()
	if client == nil {
		return failure(id, "signInRequired", "Add your SoC account in Account before checking printer queues.", nil)
	}
	printer, ok := catalog.FindQueue(printers, queue)
	if !ok || printer.Access != "public" {
		return failure(id, "invalidQueue", "Choose a queue available to student accounts.", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Second)
	defer cancel()
	snapshot, err := client.Queue(ctx, queue)
	if err != nil {
		return failure(id, "queueFailed", "Print @ SoC could not refresh this queue. Check your connection and try again.", nil)
	}
	jobs := make([]map[string]any, 0, len(snapshot.Jobs))
	for _, job := range snapshot.Jobs {
		jobs = append(jobs, map[string]any{
			"Rank": job.Rank, "Owner": job.Owner, "ID": job.ID,
			"FileName": job.FileName, "Size": job.Size, "Raw": job.Raw,
		})
	}
	return result(id, map[string]any{
		"Queue": snapshot.Queue, "Output": snapshot.Output, "CheckedAt": snapshot.CheckedAt,
		"KnownEmpty": snapshot.KnownEmpty, "Complete": snapshot.Complete,
		"Route": snapshot.Route.String(), "Jobs": jobs,
	})
}

func (a *application) cancelJob(id, operationID string) uiResponse {
	a.mu.Lock()
	client, username := a.client, a.username
	a.mu.Unlock()
	if client == nil {
		return failure(id, "signInRequired", "Add your SoC account in Account before cancelling a queued job.", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	jobs, err := a.history.List(ctx, 100)
	if err != nil {
		return failure(id, "historyFailed", "Print @ SoC could not read local job history.", nil)
	}
	var selected *store.Job
	for i := range jobs {
		if jobs[i].ID == operationID && jobs[i].Username == username {
			selected = &jobs[i]
			break
		}
	}
	if selected == nil || selected.State != "queued" || selected.SpoolerID == "" {
		return failure(id, "cancelUnavailable", "This job has no verified queue entry that can be cancelled.", nil)
	}
	if err := client.Cancel(ctx, selected.Queue, selected.SpoolerID, selected.ID); err != nil {
		return failure(id, "cancelFailed", "The queue changed or the server could not confirm cancellation. Refresh Jobs and check again.", nil)
	}
	if err := a.history.Finish(ctx, selected.ID, "cancelled", selected.SpoolerID, "Cancelled from the SoC print queue."); err != nil {
		return failure(id, "cancelHistoryFailed", "The server cancelled the job, but local history could not be updated.", nil)
	}
	return result(id, map[string]any{"Cancelled": true})
}

func (a *application) signOut(id string, forget bool) uiResponse {
	a.mu.Lock()
	client, pending := a.client, a.pendingClient
	username, settings := a.username, a.settings
	a.client, a.pendingClient, a.pendingTrust = nil, nil, nil
	a.password, a.keyPassphrase, a.route = "", "", transport.RouteUnknown
	a.epoch++
	a.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
	if pending != nil && pending != client {
		_ = pending.Close()
	}
	if forget {
		if err := credentials.DeletePassword(username); err != nil {
			return failure(id, "vaultDeleteFailed", "Keychain could not remove the saved password. Retry Forget credentials.", nil)
		}
		if err := credentials.DeletePassphrase(username); err != nil {
			return failure(id, "vaultDeleteFailed", "Keychain could not remove the saved key passphrase. Retry Forget credentials.", nil)
		}
		settings = config.Settings{TermsVersion: settings.TermsVersion}
	} else {
		settings.Username = username
	}
	if err := config.Save(settings); err != nil {
		return failure(id, "settingsFailed", "Signed out, but Print @ SoC could not save the account setting.", nil)
	}
	a.mu.Lock()
	a.settings, a.username = settings, settings.Username
	a.mu.Unlock()
	return result(id, map[string]any{"SignedOut": true, "Forgotten": forget})
}

func (a *application) close() {
	a.mu.Lock()
	client, pending := a.client, a.pendingClient
	a.client, a.pendingClient, a.pendingTrust = nil, nil, nil
	a.password, a.keyPassphrase = "", ""
	a.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
	if pending != nil && pending != client {
		_ = pending.Close()
	}
}
