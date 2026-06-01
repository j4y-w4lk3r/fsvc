package command

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/j4y-w4lk3r/fsvc/internal/config"
	"github.com/j4y-w4lk3r/fsvc/internal/revolut"
)

// RunRevolut dispatches `fsvc revolut <subsubcmd>`. Subsubcommand list
// mirrors the Python scripts in python-legacy/rev/ that the user has
// already validated work end-to-end.
func RunRevolut(g *Globals, args []string) error {
	if len(args) == 0 {
		printRevolutUsage(os.Stderr)
		return errors.New("fsvc revolut: missing subcommand")
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "exchange-code":
		return runRevolutExchangeCode(g, rest)
	case "authorize", "auth-flow":
		return runRevolutAuthorize(g, rest)
	case "auth", "refresh":
		return runRevolutAuth(g, rest)
	case "status":
		return runRevolutStatus(g, rest)
	case "accounts":
		return runRevolutAccounts(g, rest)
	case "cards":
		return runRevolutCards(g, rest)
	case "txs", "transactions":
		return runRevolutTxs(g, rest)
	case "-h", "--help", "help":
		printRevolutUsage(os.Stdout)
		return nil
	default:
		printRevolutUsage(os.Stderr)
		return fmt.Errorf("fsvc revolut: unknown subcommand %q", sub)
	}
}

func printRevolutUsage(w io.Writer) {
	fmt.Fprintf(w, `Usage: fsvc revolut <subcommand> [flags]

Subcommands:
  authorize              One-shot OAuth flow: opens browser to the consent URL,
                         catches the ?code=... redirect on a local HTTP server,
                         and exchanges the code for refresh+access tokens. Use
                         this on first setup. (Aliases: auth-flow)
  exchange-code <CODE>   Manual: trade an OAuth authorization code for tokens
                         (use this if you already have a code from another flow).
  auth                   Refresh the access_token using the stored refresh_token.
                         (Aliases: refresh)
  status                 Print local config + token freshness (no API call).
                         Useful for diagnosing setup issues without burning a
                         token refresh.
  accounts               List all Revolut Business accounts.
  cards                  List all cards.
  txs                    List transactions across all accounts (or one with --account).
                         (Aliases: transactions)

Common flags (per subcommand):
  --json                 Emit raw JSON instead of the formatted table.

Examples:
  fsvc revolut authorize                              # first-run setup (opens browser)
  fsvc revolut status                                 # what's my local state?
  fsvc revolut accounts
  fsvc revolut txs --days 30
  fsvc revolut txs --account <ACCOUNT-ID> --json | jq .
`)
}

// runRevolutExchangeCode wires the one-time OAuth code → tokens swap.
// Run this once after the user clicks Authorize in Revolut Business
// → APIs and gets bounced to <redirect_uri>?code=<CODE>; subsequent
// `fsvc revolut <cmd>` calls will refresh transparently.
//
// For most users this is plumbed via `fsvc revolut authorize` (which
// catches the redirect itself); exchange-code is the manual escape
// hatch when the OAuth dance happens out-of-band (e.g. on a different
// machine, or via an in-house OAuth proxy).
func runRevolutExchangeCode(g *Globals, args []string) error {
	fs := flag.NewFlagSet("fsvc revolut exchange-code", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return errors.New("fsvc revolut exchange-code: exactly one CODE argument required")
	}
	code := rest[0]

	cfg, err := g.LoadConfig()
	if err != nil {
		return err
	}
	if err := MustValidate(cfg); err != nil {
		return err
	}

	cli, err := revolut.New(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tok, err := cli.ExchangeAuthCode(ctx, code)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "[✓] Exchanged auth code for tokens.\n")
	fmt.Fprintf(os.Stderr, "    access_token: %s... (expires in %ds)\n", truncate(tok.AccessToken, 20), tok.ExpiresIn)
	if tok.RefreshToken != "" {
		fmt.Fprintf(os.Stderr, "    refresh_token: %s... (saved to %s)\n", truncate(tok.RefreshToken, 20), cfg.Path())
	}
	fmt.Fprintf(os.Stderr, "\nTry: fsvc revolut accounts\n")
	return nil
}

// runRevolutAuthorize is the one-shot first-run setup: spin a local
// HTTP server on the port from cfg.Revolut.RedirectURI, open the
// browser to Revolut's consent URL, and the moment Revolut bounces
// back with `?code=...` we capture the code, exchange it for tokens
// (mTLS POST to /auth/token signed with the JWT client_assertion),
// persist everything atomically to config.toml, and shut the server
// down.
//
// Why a local server instead of "paste the code into the terminal":
// it removes the most error-prone step in the flow (the user copying
// a 60+ char `oa_prod_…` value out of a browser address bar within a
// 2-minute window before the code expires). The URL the browser ends
// up at is "Bad Request"-looking even on success, which trips users
// up; the local server replaces that with a "fsvc: success — close
// this tab" page that's unambiguous.
//
// Implementation notes:
//   - Port comes from the URL in cfg, NOT from a flag — Revolut
//     enforces byte-for-byte match against the registered redirect_uri
//     so anything other than "use the configured value" is wrong.
//   - We listen ONLY on 127.0.0.1 (not 0.0.0.0) so the auth code can't
//     be intercepted by a process on another machine on the LAN
//     during the ~10s window the server is up.
//   - 5 min timeout is generous: the auth code itself has ~2 min
//     server-side lifetime, so longer waits are pointless.
func runRevolutAuthorize(g *Globals, args []string) error {
	fs := flag.NewFlagSet("fsvc revolut authorize", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	noOpen := fs.Bool("no-open", false, "don't try to auto-open the browser; just print the URL")
	timeout := fs.Duration("timeout", 5*time.Minute, "max time to wait for the redirect")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfg, err := g.LoadConfig()
	if err != nil {
		return err
	}
	if err := MustValidate(cfg); err != nil {
		return err
	}
	if cfg.Revolut.RedirectURI == "" {
		return errors.New("fsvc revolut authorize: revolut.redirect_uri is empty in config (run `fsvc init --redirect-uri ...` first)")
	}

	redirectURL, err := url.Parse(cfg.Revolut.RedirectURI)
	if err != nil {
		return fmt.Errorf("fsvc revolut authorize: invalid redirect_uri %q: %w", cfg.Revolut.RedirectURI, err)
	}

	consentURL := buildConsentURL(cfg.Revolut.ClientID, cfg.Revolut.RedirectURI)

	// Two modes, dispatched on what the redirect_uri actually points at:
	//
	//   localServerMode (http://localhost OR http://127.0.0.1):
	//       Run a local HTTP server, catch the redirect with ?code, no
	//       human paste step. Only works on Revolut Sandbox — production
	//       Revolut Business REJECTS http:// redirect_uris with the
	//       cryptic "Bad Request — please check the redirect URL" error,
	//       so registering localhost there is a dead end.
	//
	//   pasteBackMode (https://anything-else):
	//       Open the browser, the user clicks Authorize, the browser
	//       follows the 302 to <redirect_uri>?code=... — even when
	//       there's no server at that URL, the browser address bar
	//       still shows the full URL. We prompt the user to paste it
	//       back (or just the code value), parse the code, and
	//       exchange. This is the production-Revolut-Business path.
	//
	// We pick the mode automatically from the redirect_uri so users
	// don't have to know about the distinction unless they want to.
	host := strings.ToLower(redirectURL.Hostname())
	isLocalServerMode := redirectURL.Scheme == "http" &&
		(host == "localhost" || host == "127.0.0.1" || host == "::1")

	if isLocalServerMode {
		return authorizeLocalServer(cfg, redirectURL, consentURL, *noOpen, *timeout)
	}
	return authorizePasteBack(cfg, consentURL, *noOpen)
}

// authorizeLocalServer runs the catch-the-redirect flow against an
// http://localhost (or http://127.0.0.1) redirect_uri. Suitable for
// Revolut Sandbox, which accepts http://localhost; rejected by
// production Revolut Business (use authorizePasteBack for production,
// dispatched automatically by runRevolutAuthorize).
//
// Implementation notes:
//   - Port comes from the URL in cfg, NOT from a flag — Revolut
//     enforces byte-for-byte match against the registered redirect_uri
//     so anything other than "use the configured value" is wrong.
//   - We listen ONLY on 127.0.0.1 (not 0.0.0.0) so the auth code can't
//     be intercepted by a process on another machine on the LAN
//     during the ~10s window the server is up.
//   - 5 min timeout is generous: the auth code itself has ~2 min
//     server-side lifetime, so longer waits are pointless.
func authorizeLocalServer(cfg *config.Config, redirectURL *url.URL, consentURL string, noOpen bool, timeout time.Duration) error {
	port := redirectURL.Port()
	if port == "" {
		port = "80"
	}
	path := redirectURL.Path
	if path == "" {
		path = "/"
	}
	bindAddr := net.JoinHostPort("127.0.0.1", port)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r.RemoteAddr) {
			http.Error(w, "fsvc: refusing request from non-loopback origin", http.StatusForbidden)
			return
		}
		q := r.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			fmt.Fprint(w, htmlError(errParam, q.Get("error_description")))
			select {
			case errCh <- fmt.Errorf("revolut returned %s: %s", errParam, q.Get("error_description")):
			default:
			}
			return
		}
		code := q.Get("code")
		if code == "" {
			fmt.Fprint(w, htmlError("no_code", "redirect did not include a ?code=… query parameter"))
			select {
			case errCh <- errors.New("revolut redirect had no ?code= parameter"):
			default:
			}
			return
		}
		fmt.Fprint(w, htmlSuccess())
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select {
		case codeCh <- code:
		default:
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			return
		}
		http.Error(w, fmt.Sprintf("fsvc: this server is waiting for a callback at %s, not %s. Check that revolut.redirect_uri in config.toml exactly matches what you registered with Revolut.", path, r.URL.Path), http.StatusNotFound)
	})

	srv := &http.Server{
		Addr:              bindAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("fsvc revolut authorize: bind %s: %w (is another fsvc / dev server already on this port?)", bindAddr, err)
	}
	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			select {
			case errCh <- fmt.Errorf("local server: %w", serveErr):
			default:
			}
		}
	}()

	fmt.Fprintf(os.Stderr, "[*] Local server listening on http://%s%s\n", bindAddr, path)
	fmt.Fprintf(os.Stderr, "[*] Browsing to:\n    %s\n", consentURL)

	if !noOpen {
		if err := openBrowser(consentURL); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Could not auto-open the browser (%v).\n    Open the URL above manually.\n", err)
		}
	}

	fmt.Fprintf(os.Stderr, "[*] Waiting for the redirect (Ctrl+C to abort, %s timeout)...\n", timeout)

	var code string
	select {
	case code = <-codeCh:
		fmt.Fprintf(os.Stderr, "[✓] Captured authorization code (length %d)\n", len(code))
	case authErr := <-errCh:
		_ = srv.Shutdown(context.Background())
		return authErr
	case <-time.After(timeout):
		_ = srv.Shutdown(context.Background())
		return fmt.Errorf(`fsvc revolut authorize: timed out waiting for redirect after %s

The browser never bounced to %s. Most common causes:

  1. You're on production Revolut Business with an http://localhost
     redirect_uri. Production rejects http:// — only HTTPS works.
     Re-create the cert with redirect_uri = https://example.com/callback
     (or any HTTPS URL). fsvc will switch to paste-back mode automatically.

  2. The certificate is still "Access disabled" in the Revolut portal.
     Open https://business.revolut.com/settings/apis?tab=business-api,
     click your cert (CLIENT_ID prefix %s),
     and click "Enable access". Complete any push/SMS/biometric
     confirmation. The cert listing must read "Access enabled" before
     OAuth will succeed.

  3. You closed the browser tab before clicking Authorize.
     Just rerun this command.`,
			timeout,
			cfg.Revolut.RedirectURI,
			truncate(cfg.Revolut.ClientID, 12),
		)
	}

	go func() {
		time.Sleep(250 * time.Millisecond)
		_ = srv.Shutdown(context.Background())
	}()

	return finishExchange(cfg, code)
}

// authorizePasteBack runs the production-friendly OAuth flow: open
// the browser, let Revolut redirect to the user's chosen HTTPS URL
// (which may or may not actually serve anything — even a static page
// or a 404 is fine), then ask the user to paste back either the
// FULL redirect URL or just the `code` query param value.
//
// Why this works without infrastructure: Revolut's auth server issues
// a 302 to the registered redirect_uri with `?code=...`. The browser
// follows that 302, displays whatever page the redirect_uri serves
// (often "couldn't connect" or a real site like example.com), and
// crucially shows the FULL URL in the address bar including the
// `?code=...`. The user copies that URL and we extract the code.
//
// The user's previous Python workflow used exactly this shape (with
// a public IP redirect_uri) — `revolut_exchange_code.py` accepted the
// code as a CLI arg. We're just polishing it: build the consent URL,
// open the browser, and read stdin instead of requiring a separate
// command invocation.
func authorizePasteBack(cfg *config.Config, consentURL string, noOpen bool) error {
	fmt.Fprintf(os.Stderr, "[*] Mode: paste-back (redirect_uri %s is not http://localhost)\n", cfg.Revolut.RedirectURI)
	fmt.Fprintf(os.Stderr, "[*] Browsing to:\n    %s\n", consentURL)

	if !noOpen {
		if err := openBrowser(consentURL); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Could not auto-open the browser (%v).\n    Open the URL above manually.\n", err)
		}
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "After clicking Authorize in the browser, Revolut will redirect to:")
	fmt.Fprintf(os.Stderr, "    %s?code=oa_prod_<long-string>\n", cfg.Revolut.RedirectURI)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "The browser may show 'can't connect' or a real page — either is fine.")
	fmt.Fprintln(os.Stderr, "Copy the FULL URL from the address bar (or just the `code` value) and paste it below.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprint(os.Stderr, "Paste here, then Enter: ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("fsvc revolut authorize: reading from stdin: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return errors.New("fsvc revolut authorize: empty input")
	}

	code, err := extractCodeFromInput(line)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[✓] Captured authorization code (length %d)\n", len(code))

	return finishExchange(cfg, code)
}

// finishExchange is the shared "we have the code, now swap it for
// tokens" tail of both authorize modes. Pulled out so the local-
// server and paste-back paths converge instead of duplicating the
// mTLS-POST-and-Save logic.
func finishExchange(cfg *config.Config, code string) error {
	cli, err := revolut.New(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Fprintf(os.Stderr, "[*] Exchanging code for tokens (mTLS + JWT client_assertion)...\n")
	tok, err := cli.ExchangeAuthCode(ctx, code)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "[✓] access_token: %s… (expires in %ds)\n", truncate(tok.AccessToken, 18), tok.ExpiresIn)
	if tok.RefreshToken != "" {
		fmt.Fprintf(os.Stderr, "[✓] refresh_token saved to %s\n", cfg.Path())
	}
	fmt.Fprintf(os.Stderr, "\nReady. Try:\n    fsvc revolut accounts\n    fsvc revolut txs --days 30\n")
	return nil
}

// extractCodeFromInput pulls a Revolut OAuth `code` value out of the
// flexible input the user might paste back: either the full redirect
// URL (with `?code=...` somewhere) or the bare code value itself.
//
// Revolut codes look like `oa_prod_xxxxxxxxxx` (production) or
// `oa_sand_xxxxxxxxxx` (sandbox), opaque base64-ish strings. We
// don't validate the prefix because Revolut may evolve the format;
// "starts with http(s)://" is the only meaningful branch.
func extractCodeFromInput(input string) (string, error) {
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err != nil {
			return "", fmt.Errorf("invalid URL %q: %w", input, err)
		}
		// Some redirect_uris use the fragment instead of the query
		// (Revolut's response_mode=fragment option). Check both.
		code := u.Query().Get("code")
		if code == "" && u.Fragment != "" {
			frag, _ := url.ParseQuery(u.Fragment)
			code = frag.Get("code")
		}
		if code == "" {
			return "", fmt.Errorf("URL %q has no `?code=...` parameter — did you paste the URL BEFORE clicking Authorize?", input)
		}
		return code, nil
	}
	// Bare code. Trim any quotes a copy-paste tool might wrap with.
	return strings.Trim(input, `"'`), nil
}

// buildConsentURL constructs the Revolut Business consent URL with all
// four required query params. Done as a separate function so it can
// be tested without having to spin up an HTTP server. URL-encoding
// is delegated to net/url so a redirect_uri with weird-but-legal
// characters (semicolons, spaces in hostnames, ...) is handled
// correctly.
//
// Required params (Revolut docs):
//   - client_id      = the CLIENT_ID issued for this cert
//   - response_type  = "code" (this is the authorization-code flow;
//     omit it and Revolut returns the cryptic
//     "Bad Request - Please check the redirect URL"
//     error that costs everybody 20 minutes to debug)
//   - redirect_uri   = byte-for-byte match to what's registered
//   - scope          = "READ" (READ-only access; tighter than the
//     default to limit the blast radius if a token
//     leaks)
func buildConsentURL(clientID, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "READ")
	return "https://business.revolut.com/app-confirm?" + q.Encode()
}

// openBrowser launches the OS-default browser at `target`. Best-
// effort: we don't wait for the process or check exit code (the user
// may have closed the browser window the moment it opened, etc.).
// Each platform has its own incantation; on unsupported platforms we
// return an error so the caller can fall back to "paste this URL".
func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "linux":
		cmd = exec.Command("xdg-open", target)
	case "windows":
		// rundll32 is the classic Windows idiom for "ask the OS to
		// open this URL with the default handler".
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		return fmt.Errorf("don't know how to open a browser on %s", runtime.GOOS)
	}
	return cmd.Start()
}

// isLoopback reports whether `remote` (an http.Request.RemoteAddr-
// shape "ip:port") originates from a loopback IP. Belt-and-braces:
// even though we already bind to 127.0.0.1, on some Linux setups the
// Listener can still see traffic bridged from container networks etc.
func isLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		// Some test servers pass just the IP. Try parsing whole.
		host = remote
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// htmlSuccess renders the "you can close this tab" page shown in the
// browser the moment we've captured the auth code. Inline minimal
// styles so it doesn't look like a 1995 Apache default. Self-
// contained — no remote fonts, no JS, no fetches. Loads instantly
// even if the local server is about to shut down.
func htmlSuccess() string {
	return `<!doctype html><meta charset="utf-8"><title>fsvc: authorized</title>` +
		`<style>` +
		`html,body{margin:0;height:100%;display:grid;place-items:center;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#0b0d12;color:#eaeaea}` +
		`.card{padding:32px 40px;border:1px solid #1f2530;border-radius:14px;background:#11141b;text-align:center;max-width:520px}` +
		`h1{margin:0 0 8px;font-weight:600;font-size:20px}` +
		`p{margin:8px 0;color:#9aa3b2}` +
		`code{background:#0b0d12;padding:2px 6px;border-radius:4px;font-size:13px;color:#e0e6f0}` +
		`</style>` +
		`<div class="card"><h1>fsvc: authorization complete</h1>` +
		`<p>You can close this tab. The terminal has the rest.</p>` +
		`<p><code>fsvc revolut accounts</code> should now work.</p></div>`
}

// htmlError renders the equivalent "we caught the redirect, but
// something was off" page. Keeps the same visual shell so the
// success/failure delta is unmistakable.
func htmlError(code, description string) string {
	if description == "" {
		description = "(no description from Revolut)"
	}
	return `<!doctype html><meta charset="utf-8"><title>fsvc: authorization failed</title>` +
		`<style>` +
		`html,body{margin:0;height:100%;display:grid;place-items:center;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#0b0d12;color:#eaeaea}` +
		`.card{padding:32px 40px;border:1px solid #5a1f25;border-radius:14px;background:#1b1115;text-align:center;max-width:560px}` +
		`h1{margin:0 0 8px;font-weight:600;font-size:20px;color:#ff7a85}` +
		`p{margin:8px 0;color:#caa9ad}` +
		`code{background:#0b0d12;padding:2px 6px;border-radius:4px;font-size:13px;color:#e0e6f0}` +
		`</style>` +
		`<div class="card"><h1>fsvc: authorization failed</h1>` +
		`<p>Revolut returned <code>` + htmlEscape(code) + `</code></p>` +
		`<p>` + htmlEscape(description) + `</p>` +
		`<p>Check the terminal for next steps.</p></div>`
}

// htmlEscape is a tiny html-escape shim so we don't import html for
// just two callers. Strings.Replacer would also work; this is
// clearer for the small set we care about.
func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// runRevolutStatus prints the current local state: config path, the
// CLIENT_ID prefix (so the user can match it against the Revolut
// portal), the redirect URI, and access-token freshness. Doesn't hit
// the API — calling Revolut just to render a status line would burn
// a token refresh and rate-limit budget for no reason.
//
// Designed to be the answer to "wait, which config is fsvc using?
// is my token current? did `fsvc init` actually take?". Cheap to
// run, no auth side-effects.
func runRevolutStatus(g *Globals, args []string) error {
	fs := flag.NewFlagSet("fsvc revolut status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfg, err := g.LoadConfig()
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "config\t%s\n", cfg.Path())
	fmt.Fprintf(tw, "base_url\t%s\n", cfg.Revolut.BaseURL)
	if cfg.Revolut.ClientID != "" {
		fmt.Fprintf(tw, "client_id\t%s\n", redactMiddle(cfg.Revolut.ClientID, 12, 4))
	} else {
		fmt.Fprintf(tw, "client_id\t(empty — run `fsvc init`)\n")
	}
	fmt.Fprintf(tw, "iss\t%s\n", or(cfg.Revolut.ISS, "(empty)"))
	fmt.Fprintf(tw, "redirect_uri\t%s\n", or(cfg.Revolut.RedirectURI, "(empty)"))
	fmt.Fprintf(tw, "cert_path\t%s\t%s\n", cfg.Revolut.CertPath, statTag(cfg.Revolut.CertPath))
	fmt.Fprintf(tw, "key_path\t%s\t%s\n", cfg.Revolut.KeyPath, statTag(cfg.Revolut.KeyPath))

	switch {
	case cfg.Revolut.AccessToken == "":
		fmt.Fprintf(tw, "access_token\t(none — run `fsvc revolut authorize`)\n")
	case cfg.Revolut.AccessExpiresAt.IsZero():
		fmt.Fprintf(tw, "access_token\t(present, no recorded expiry — run `fsvc revolut auth`)\n")
	default:
		remaining := time.Until(cfg.Revolut.AccessExpiresAt).Round(time.Second)
		state := "fresh"
		if remaining <= 0 {
			state = "EXPIRED"
		} else if remaining < 60*time.Second {
			state = "expiring soon"
		}
		fmt.Fprintf(tw, "access_token\tpresent (%s, %s)\t(expires %s)\n",
			state, remaining, cfg.Revolut.AccessExpiresAt.Format(time.RFC3339))
	}

	if cfg.Revolut.RefreshToken == "" {
		fmt.Fprintf(tw, "refresh_token\t(none — run `fsvc revolut authorize`)\n")
	} else {
		fmt.Fprintf(tw, "refresh_token\tpresent (%s)\n", redactMiddle(cfg.Revolut.RefreshToken, 8, 4))
	}
	tw.Flush()

	// Roll-up problem list (same source of truth as MustValidate, but
	// rendered as advice rather than as a fatal error).
	if probs := cfg.Validate(); len(probs) > 0 {
		fmt.Println()
		fmt.Println("Issues to resolve before API calls will work:")
		for _, p := range probs {
			fmt.Printf("  - %s\n", p)
		}
	}
	return nil
}

// or returns s if non-empty, otherwise fallback. Tiny helper to keep
// status print statements from being littered with ternary-ish
// expressions.
func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// statTag returns "ok" / "missing" / "unreadable" for a file path,
// suitable for inline rendering in `fsvc revolut status`. Centralizes
// the "is this path a real readable file?" check we'd otherwise
// duplicate across cert_path / key_path lines.
func statTag(path string) string {
	if path == "" {
		return "(empty)"
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "MISSING"
		}
		return fmt.Sprintf("UNREADABLE (%v)", err)
	}
	if info.IsDir() {
		return "(is a directory??)"
	}
	return "ok"
}

// runRevolutAuth refreshes the access_token using the stored
// refresh_token. Useful as a manual "warm the cache" before a series
// of commands, or to confirm the credentials still work without
// hitting a real endpoint.
func runRevolutAuth(g *Globals, args []string) error {
	fs := flag.NewFlagSet("fsvc revolut auth", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfg, err := g.LoadConfig()
	if err != nil {
		return err
	}
	if err := MustValidate(cfg); err != nil {
		return err
	}
	if cfg.Revolut.RefreshToken == "" {
		return errors.New("fsvc revolut auth: no refresh_token in config; run `fsvc revolut exchange-code <CODE>` first")
	}

	cli, err := revolut.New(cfg)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tok, err := cli.RefreshAccessToken(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "[✓] Access token refreshed (expires in %ds).\n", tok.ExpiresIn)
	if tok.RefreshToken != "" && tok.RefreshToken != cfg.Revolut.RefreshToken {
		// Already saved by RefreshAccessToken; just inform the user.
		fmt.Fprintf(os.Stderr, "[*] Refresh token rotated; new value persisted to %s\n", cfg.Path())
	}
	return nil
}

// runRevolutAccounts implements `fsvc revolut accounts`. Default
// output: a tabwriter-aligned table with the most useful columns.
// `--json` switches to raw JSON (the full payload), useful for piping
// into jq.
func runRevolutAccounts(g *Globals, args []string) error {
	fs := flag.NewFlagSet("fsvc revolut accounts", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit raw JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cli, cfg, err := loadClient(g)
	if err != nil {
		return err
	}
	_ = cfg

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	accounts, err := cli.GetAccounts(ctx)
	if err != nil {
		return err
	}

	if *asJSON {
		return writeJSON(os.Stdout, accounts)
	}

	// Sort by currency then name so the same-currency accounts cluster
	// together — easiest to scan when you have several EUR / GBP / USD
	// accounts.
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Currency != accounts[j].Currency {
			return accounts[i].Currency < accounts[j].Currency
		}
		return accounts[i].Name < accounts[j].Name
	})

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CURRENCY\tBALANCE\tNAME\tSTATE\tID")
	var totals = map[string]float64{}
	for _, a := range accounts {
		fmt.Fprintf(tw, "%s\t%.2f\t%s\t%s\t%s\n", a.Currency, a.Balance, a.Name, a.State, a.ID)
		totals[a.Currency] += a.Balance
	}
	fmt.Fprintln(tw, "\t\t\t\t")
	tw.Flush()

	// Per-currency rollups — one of the things the user explicitly
	// asked for ("nicely list ... the money on the different
	// accounts").
	if len(totals) > 0 {
		ccys := make([]string, 0, len(totals))
		for c := range totals {
			ccys = append(ccys, c)
		}
		sort.Strings(ccys)
		fmt.Println("Totals:")
		for _, c := range ccys {
			fmt.Printf("  %s  %.2f\n", c, totals[c])
		}
	}
	return nil
}

// runRevolutCards implements `fsvc revolut cards`.
func runRevolutCards(g *Globals, args []string) error {
	fs := flag.NewFlagSet("fsvc revolut cards", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit raw JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cli, _, err := loadClient(g)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cards, err := cli.GetCards(ctx)
	if err != nil {
		return err
	}

	if *asJSON {
		return writeJSON(os.Stdout, cards)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATE\tBRAND\tLAST4\tLABEL\tACCOUNT-ID\tID")
	for _, c := range cards {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			c.State, c.Brand, c.LastFour, c.Label, c.AccountID, c.ID)
	}
	tw.Flush()
	return nil
}

// runRevolutTxs implements `fsvc revolut txs`. Time-window options
// mirror the Python REVOLUT_FROM / REVOLUT_TO / REVOLUT_DAYS_BACK env
// vars (still honored via config.applyEnvOverrides paths) but exposed
// as flags too for convenience.
func runRevolutTxs(g *Globals, args []string) error {
	fs := flag.NewFlagSet("fsvc revolut txs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit raw JSON instead of a table")
	days := fs.Int("days", 365, "lookback window in days (ignored if --from is set)")
	fromStr := fs.String("from", "", "RFC 3339 start time (e.g. 2026-01-01T00:00:00Z)")
	toStr := fs.String("to", "", "RFC 3339 end time (defaults to now)")
	accountID := fs.String("account", "", "limit to a specific account ID")
	count := fs.Int("count", 1000, "max transactions per account (Revolut caps at 1000)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cli, _, err := loadClient(g)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	to := now
	if *toStr != "" {
		t, err := time.Parse(time.RFC3339, *toStr)
		if err != nil {
			return fmt.Errorf("fsvc revolut txs: --to must be RFC 3339: %w", err)
		}
		to = t.UTC()
	}
	from := to.AddDate(0, 0, -*days)
	if *fromStr != "" {
		t, err := time.Parse(time.RFC3339, *fromStr)
		if err != nil {
			return fmt.Errorf("fsvc revolut txs: --from must be RFC 3339: %w", err)
		}
		from = t.UTC()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// We could in principle just hit /transactions once with no
	// `account` param to get all of them in one shot, but the Python
	// code chose per-account fan-out for nicer grouping, and that's
	// what the user is used to seeing. Replicating that here.
	var queryAccounts []revolut.Account
	if *accountID != "" {
		queryAccounts = []revolut.Account{{ID: *accountID}}
	} else {
		accs, err := cli.GetAccounts(ctx)
		if err != nil {
			return err
		}
		queryAccounts = accs
	}

	type accountTxs struct {
		Account      revolut.Account
		Transactions []revolut.Transaction
	}
	var grouped []accountTxs
	for _, a := range queryAccounts {
		txs, err := cli.GetTransactions(ctx, revolut.TransactionsOpts{
			AccountID: a.ID,
			From:      from,
			To:        to,
			Count:     *count,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] account %s: %v\n", a.ID, err)
			continue
		}
		grouped = append(grouped, accountTxs{Account: a, Transactions: txs})
	}

	if *asJSON {
		return writeJSON(os.Stdout, grouped)
	}

	fmt.Printf("Window: %s → %s\n", from.Format(time.RFC3339), to.Format(time.RFC3339))
	for _, g := range grouped {
		fmt.Println()
		label := g.Account.Name
		if label == "" {
			label = g.Account.ID
		}
		fmt.Printf("=== %s (%s) — %d tx ===\n", label, g.Account.Currency, len(g.Transactions))
		if len(g.Transactions) == 0 {
			fmt.Println("  (no transactions in window)")
			continue
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  CREATED\tTYPE\tSTATE\tAMOUNT\tDESCRIPTION")
		for _, t := range g.Transactions {
			amount, ccy := t.AmountAndCurrency()
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%.2f %s\t%s\n",
				shortTime(t.CreatedAt), t.Type, t.State, amount, ccy, truncate(t.Description(), 60))
		}
		tw.Flush()
	}
	return nil
}

// loadClient is the shared "open config + build mTLS client" helper.
// Returns the client AND the config so callers that need to read
// config fields (e.g. paths) don't have to re-load.
func loadClient(g *Globals) (*revolut.Client, any, error) {
	cfg, err := g.LoadConfig()
	if err != nil {
		return nil, nil, err
	}
	if err := MustValidate(cfg); err != nil {
		return nil, nil, err
	}
	cli, err := revolut.New(cfg)
	if err != nil {
		return nil, nil, err
	}
	return cli, cfg, nil
}

// writeJSON pretty-prints v to w with 2-space indent. Used by all
// --json output paths. Pretty by default because the alternative
// (compact) is hard to read in a terminal and `jq -c` is one
// keystroke away if you really want compact.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// shortTime turns a Revolut RFC-3339 timestamp into "YYYY-MM-DD HH:MM"
// for the table view. Falls back to the original string if parsing
// fails (Revolut occasionally varies the suffix).
func shortTime(s string) string {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return s
		}
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// truncate shortens a string to n runes, appending "…" if it was
// truncated. Used to keep the description column from wrapping the
// table.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return strings.TrimRight(string(rs[:n-1]), " ") + "…"
}
