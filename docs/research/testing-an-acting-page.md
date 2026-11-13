# Testing a page that acts

Research for [#206](https://github.com/probertson/diff-by-numbers/issues/206), part of the
Browser App map [#198](https://github.com/probertson/diff-by-numbers/issues/198).
Researched 2026-11-12 against chromedp v0.23.0, go-rod v0.116.2, playwright-go v0.6401.0
(Playwright driver 1.64.1), Playwright v1.65.0, Vitest v5.1.0, jsdom v31.0.0 and happy-dom
v21.0.4, with the repo at `de86779` (v0.6.3).

**Question.** Today the page's tests are Go tests over the HTML the daemon serves, and
`page.js` has none. Once the script handles keys, drafts and buttons, how can its behavior be
tested here: a browser driven from Go, a JS test runner, or a thin script whose logic lives
in Go? What does each cost in CI and in a contributor's setup?

## Short answer

- **Do both ends: keep the script thin, and test what is left in a real browser driven
  from Go with chromedp.** Neither alone covers the map. A thin script moves most of the
  logic (what a key does, the cursor, draft flags) into Go, where the existing httptest
  tests reach it. But the parts that stay in the script are the browser's own behavior:
  which keys the page takes from the browser, focus, cmd/ctrl+Enter and Esc in a text box,
  the hover button, following the change stream. Only a real browser can verify those.
- **chromedp fits this repo best.** It is pure Go, so no Node toolchain and no
  `package.json`. It drives the Chrome that GitHub's `ubuntu-latest` image already ships
  [R1], so CI needs no new step. It reuses the daemon's own test helpers in-process. It is
  actively released (v0.23.0, 2026-11-05) [C1]. In a spike (below) keys, a text box with
  ctrl+Enter, focus, a server-sent event, layout and a hover button all worked. A test took
  about 0.85 s with browser launch, and it passed 30 runs out of 30 under `-race`.
- **A JS test runner costs the most and adds the least here.** Playwright Test is the
  strongest browser tool on offer. But it brings Node, `package.json`, a lockfile, browser
  downloads of "a few hundred megabytes" each [P4] and a second CI job. Its tests would have
  to start `dbn serve` and set up reviews over HTTP, without the Go helpers that do that
  today. Vitest with jsdom or happy-dom can't see layout or the change stream at all.
- **Of the Go drivers, go-rod is effectively unmaintained.** Its last release was v0.116.2
  on 2024-07-12, and its last code change was on 2024-12-07 [D1]. playwright-go works, but it
  downloads a ~50 MB Node-based driver plus browsers [W1] and trails upstream Playwright.
- **Cost of the recommendation.** One Go dependency (chromedp, plus `cdproto` and
  `go-json-experiment/json` indirectly). Contributors need Chrome or Chromium installed. The
  browser tests skip when no browser is found, except in CI, where they must run.

## What the page will have to do

From the decisions in #199, #200 and #201:

| Behavior | Where its logic can live | Needs a real browser to verify? |
| --- | --- | --- |
| What a key does (move, select, `c`, re-raise, Hand Off) | Go, if the page sends keys to the daemon | No, once in Go |
| The page takes over space, the arrows and shift+arrows; leaves q and ctrl+c alone | The script (`preventDefault` on `keydown`) | **Yes**: it's the browser's default action that is being stopped |
| One cursor per review, every window follows it | Go (the daemon holds it) | The following does: SSE in, the page redraws |
| A text box: Enter adds a line, cmd/ctrl+Enter saves, Esc cancels | The script | **Yes**: focus, the textarea's own Enter, modifier keys |
| Keys typed in a text box don't move the cursor | The script | **Yes** |
| A window with a draft open holds still; it tells the daemon it has one | Flag in Go; holding still in the script | The holding still does |
| Hover "add comment" button in the right margin; click opens and focuses the text box | The script and CSS | **Yes**: `:hover`, layout, focus |
| A drag is text selection only; a click on content does nothing | The browser, by default | **Yes**, if it's to be guarded |
| Keybar from one list both surfaces share | Go (served into the page) | No |

The column on the right is the point. Every option below can test the left column's Go
half. They differ on the rows marked **Yes**.

## The repo today

- **CI** is one workflow, `ci.yml`, on `ubuntu-latest`: `gofmt`, `go vet ./...` and
  `go test -race ./...`. `release-check.yml` and `release.yml` run GoReleaser. There is no
  Node setup anywhere, and no `package.json`.
- **`ubuntu-latest` already has browsers.** The Ubuntu 24.04 image (version 20261101.360.1)
  lists Google Chrome 156.0.8078.79, Chromium 156.0.8078.0, ChromeDriver, Microsoft Edge,
  Firefox 159.0, and Node.js 22.23.4 with npm 10.9.9. Playwright isn't listed [R1].
- **The page's tests** live in `internal/daemon` (package `daemon_test`). They start
  `httptest.NewServer(daemon.New().Handler())`, create a review with helpers such as
  `onANewDocument`, read the change stream with `follow`/`nextEvent`, post cursor reports
  with `reportCursor`, then fetch `/page/stop` and assert on its HTML. The package takes
  about 43 s locally.
- **`page.js`** (448 lines) is one immediately invoked function. It reads `document.body`'s
  `data-*` URLs at load, opens an `EventSource`, and uses `getBoundingClientRect`,
  `ResizeObserver`, `matchMedia` and `localStorage`. It's inlined into the page and allowed
  by a hash in a strict Content-Security-Policy (`pagePolicy` in `browser.go`).

## Option A: a thin script, its logic in Go

The page would send what happened (a key with its modifiers, a button pressed, a draft
opened or closed) to the daemon, and the daemon would decide what it means. This matches
#200, where the daemon already holds the cursor, and #199's "one shared list" for the
keybar. The TUI's key handling (`internal/tui/tui.go`, a `switch` on `msg.String()`) would
move to a package both the TUI and the daemon call.

**Can verify:** everything decided in Go. A test posts `j` or `space` and checks the
cursor, posts a draft flag and checks that Hand Off warns, and reads the stream for the
cursor event. That's today's style, with today's helpers, at today's speed.

**Can't verify:** anything the browser does. Whether `keydown` reached the script at all,
whether `preventDefault` stopped space from scrolling, whether a key typed into a textarea
leaked to the page, whether the text box has focus after the hover button, whether
cmd/ctrl+Enter saves rather than adding a line, whether `EventSource` reconnects. A typo in
an event name passes every Go test.

**Cost:** none in CI or setup. It costs design: a round trip per key (on localhost, small),
and the key logic has to come out of the TUI's model.

**Verdict:** worth doing for its own sake. It's what lets the TUI and the page act alike,
and it shrinks what needs a browser. On its own it leaves the risky part untested.

## Option B: a browser driven from Go

### chromedp (recommended)

- **What it is.** A Go client for the Chrome DevTools Protocol. v0.23.0 was released
  2026-11-05 and needs Go 1.25 or newer [C1]. The repo is on Go 1.27.
- **Finding the browser.** chromedp looks up `headless_shell`, `chromium`,
  `chromium-browser`, `google-chrome`, `google-chrome-stable` and a few more on `PATH` on
  Linux, and `/Applications/Chromium.app` and `/Applications/Google Chrome.app` on macOS
  (`execLocations` in `allocate.go`) [C2]. On `ubuntu-latest` that finds the preinstalled
  `google-chrome` [R1]. It runs headless by default [C1].
- **Input.** `KeyEvent` takes keys from the `kb` package with `KeyModifiers` (`ModifierCtrl`,
  `ModifierMeta`, `ModifierShift` and so on) [C2]. `SendKeys` types into an element, and
  `MouseEvent` moves the pointer. One known oddity: `SendKeys` sends `\n` as Enter with a
  char event, which can add two line breaks in rich editors. The README's workaround is a
  raw `input.DispatchKeyEvent` [C1]. A plain `<textarea>` isn't affected.
- **Waiting.** `WaitVisible` and friends poll for a selector, so a test waits for the page
  to show a state rather than sleeping.
- **Fit with the repo.** It runs inside `go test`. A browser test can start
  `daemon.New().Handler()` on httptest, reuse `onANewDocument` and `reportCursor`, point
  Chrome at the page, press keys, and then assert both on the DOM (through `Evaluate`) and on
  the daemon's state through the same HTTP calls the current tests make.
- **API churn.** It is pre-1.0. v0.23.0 made the API generic: `Run` now returns `(T,
  error)`, and `Do` runs actions that return nothing. Code written against older examples
  doesn't compile [C2]. Expect to adjust tests on upgrades.

### go-rod

Rod also speaks DevTools Protocol from Go. Its launcher can "automatically find or
download" a browser [D1], which would spare contributors an install. But its last release is
v0.116.2 (2024-07-12), and its last code change was on 2024-12-07; the commits since are
sponsor and doc updates [D1]. For a new test layer that's too much risk.

### playwright-go

It brings Playwright's auto-waiting and its Firefox and WebKit to Go. Setup is a
`go run github.com/playwright-community/playwright-go/cmd/playwright@<version> install
--with-deps`, which downloads "a Node.js runtime combined with Playwright ... (around
50MB)" and the browsers [W1]. "Each minor version upgrade requires a specific Playwright
driver version" [W1]. v0.6401.0 uses driver 1.64.1 (`playwrightCliVersion` in `run.go`)
[W2], one minor release behind upstream's v1.65.0 [P1]. Playwright can use an installed
Chrome through `channel: 'chrome'` [P4], but the driver still has to be downloaded. So it
costs a CI step and a contributor setup step that chromedp doesn't. WebKit is the main
thing it would add, and nothing in the map calls for it.

### Spike: chromedp against a stand-in page

A throwaway Go test (not committed) served a page from `httptest` with the shapes the map
calls for, and drove local Chrome 157.0.8103.52 on macOS with chromedp v0.23.0:

- `j` and space on the page posted a move and a select; space's `preventDefault` ran.
- In a focused `<textarea>`, `SendKeys` typed text and ctrl+Enter posted a save. The page
  posted nothing else, so keys typed in the box didn't leak to the page.
  `document.activeElement` was still the box.
- A `cursor` event sent from the Go handler over `text/event-stream` changed a `data-*`
  attribute, and `WaitVisible` saw it.
- `getBoundingClientRect().height` was 18–21 px, not 0.
- Moving the pointer over a block with `MouseEvent(mouseMoved)` switched a `:hover` rule
  that made its button visible.

The first run took 9 s (Chrome's cold start). After that, browser launch plus page load
took about 0.5 s, the checks took 60–110 ms, and a whole test took about 0.85 s.
`go test -race -count=20` passed 20 of 20 in 17 s, and a later `-count=10` with the hover
check passed 10 of 10. Sharing one browser across a package's tests (one allocator in
`TestMain`, a new tab per test) should cut the launch cost to once per package. That wasn't
measured.

## Option C: a JS test runner

### Playwright Test

- **Strengths.** Auto-waiting locators and web-first assertions [P3], `ControlOrMeta` that
  "resolves to `Control` on Windows and Linux and to `Meta` on macOS" [P5], tracing,
  Chromium, Firefox and WebKit. `webServer` can start any shell command, so it could start
  `go run ./cmd/dbn serve`, wait for a URL or a log line, and capture the port [P2].
- **Setup.** `npm init playwright@latest` adds `playwright.config.ts`, `package.json`, a
  lockfile and a `tests/` folder, and downloads browsers [P1]. It needs Node 22.x, 24.x or
  26.x [P1].
- **CI.** The documented workflow is `actions/setup-node`, `npm ci`,
  `npx playwright install --with-deps` and `npx playwright test` [P6]. Its docs suggest
  installing only the browsers you need on CI [P3]. `--only-shell` limits the download to the
  headless shell [P4]. That is a second job beside `ci.yml`, with its own cache to manage.
- **Fit.** It's poor here. Tests are TypeScript in a Go-only repo. They can't call
  `onANewDocument` or the daemon's types, so every test sets up its review through the
  daemon's HTTP or MCP surface, and those helpers get written twice. `go test ./...` no longer
  runs every test.

### Vitest with jsdom or happy-dom

- **Layout is missing.** jsdom lists layout as out of scope and returns "zeros for many
  layout-related properties" [J1]. happy-dom's `getBoundingClientRect` returns an empty
  `DOMRect` ("TODO: Not full implementation"), and its `ResizeObserver.observe` is "Not
  implemented" [H1]. `reveal()`, `markComments()` and `widen()` in `page.js` depend on both.
- **The change stream is missing.** jsdom implements no `EventSource`; its source tree has
  none [J2]. Node's own `EventSource` is experimental behind `--experimental-eventsource`
  [N1].
- **Events are synthetic.** An event from `dispatchEvent` has `isTrusted` false [M1], and
  user-event "simulates full interactions" over that [T1]. So what the browser itself does on
  a key (scroll on space, Enter in a textarea, `:hover`) is emulated or absent. That is the
  behavior the map most needs checked.
- **The CSP isn't enforced**, so a script blocked in a real browser by `pagePolicy` would
  pass.
- **`page.js` would have to change shape.** As one immediately invoked function bound to
  the live document, it has no units to import. It would need splitting into modules.
- **Setup:** Node (jsdom 31 needs `^22.22.2 || ^24.15.0 || >=26` [J3]; Vitest needs
  `^22.12.0 || ^24.0.0 || >=26` [V2]) and a `package.json`.

### Vitest browser mode

Since Vitest 4 it's stable, with providers as separate packages
(`@vitest/browser-playwright`, `@vitest/browser-webdriverio`, `@vitest/browser-preview`)
[V1]. With Playwright or WebdriverIO it drives a real browser over CDP or WebDriver. The
`preview` provider "relies on simulating events" and can't run in CI [V3]. It has the same
Node and browser-download costs as Playwright Test, and it's built for testing modules in
isolation. Here the behavior under test is the served page talking to the daemon.

### Also considered: goja

A JS interpreter in pure Go (`dop251/goja`) could run `page.js`'s pure functions inside
`go test`. It has no DOM, events or layout, so it would only test what Option A already
moves to Go. Not pursued.

## Comparison

| | Thin script + httptest | chromedp | playwright-go | Playwright Test | Vitest + jsdom/happy-dom |
| --- | --- | --- | --- | --- | --- |
| Key handling and `preventDefault` | Go half only | Yes, real events | Yes | Yes | Emulated |
| Focus, text box keys | No | Yes | Yes | Yes | Emulated |
| Change stream (SSE) | Server side only | Yes | Yes | Yes | No `EventSource` |
| Layout, `:hover` | No | Yes | Yes | Yes | No |
| CSP enforced | No | Yes | Yes | Yes | No |
| Browsers | n/a | Chrome or Chromium | Chromium, Firefox, WebKit | Chromium, Firefox, WebKit | n/a |
| New CI steps | None | None (Chrome is on the image) | Driver and browser install | Node, `npm ci`, browser install, second job | Node, `npm ci` |
| Contributor setup | None | Chrome installed | One `go run ... install` | Node + `npm ci` + browsers | Node + `npm ci` |
| Reuses Go test helpers | Yes | Yes | Yes | No | No |
| Speed | ms per test | ~0.85 s per test with launch (spike) | Similar, plus driver process | Similar, plus Node | Fast |
| Flakiness | Low | Low in spike (30/30); waits by polling | Low; auto-waits | Low; auto-waits | Low, but tests a fake |
| Maintenance | n/a | Active, pre-1.0 API churn | Community, trails upstream | Microsoft, active | Active |

## Recommendation

1. **Design the Browser App with a thin script (Option A).** The page reports key events
   (key and modifiers), button presses and draft open/close to the daemon. Go decides what
   they mean, holds the cursor, and serves the keybar's list and the set of keys the page
   takes from the browser. Most new behavior is then tested where it is today.
2. **Add a small layer of chromedp tests for the rest (Option B).** Cover each row marked
   **Yes** in the first table: a key on the page reaches the daemon and space doesn't
   scroll; keys in a text box stay there; cmd/ctrl+Enter saves and Esc cancels; the hover
   button opens and focuses a text box on its block; a window with a draft holds still on
   a move made elsewhere; the page follows a cursor event.
3. **Shape of those tests.** Put them in `internal/daemon` beside today's, behind a helper
   that skips when chromedp can't find a browser, unless `CI` is set, so CI can't silently
   skip them. Share one browser per package from `TestMain`. They run in the existing
   `go test -race ./...` step with no workflow change.
4. **Revisit if** cross-browser behavior (Safari's WebKit, Firefox) becomes a requirement.
   Then playwright-go is the Go-side way to get it, and Playwright Test the fuller one.

Not verified here: behavior on Firefox and Safari; a shared browser's per-test cost; the
spike on a CI runner rather than macOS. The first browser test in CI will show the
runner-side timing.

## Sources

- [R1] actions/runner-images, Ubuntu 24.04 image readme, version 20261101.360.1:
  https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md
- [C1] chromedp README and FAQ (v0.23.0, released 2026-11-05):
  https://github.com/chromedp/chromedp
- [C2] chromedp v0.23.0 source: `allocate.go` (`findExecPath`, `execLocations`),
  `input.go` (`KeyEvent`, `KeyModifiers`, `MouseEvent`), `action.go` (`Run`, `Do`):
  https://github.com/chromedp/chromedp/tree/v0.23.0
- [D1] go-rod README, releases and commit history: https://github.com/go-rod/rod,
  https://github.com/go-rod/rod/commits/main
- [W1] playwright-go README: https://github.com/playwright-community/playwright-go
- [W2] playwright-go `run.go` (`playwrightCliVersion = "1.64.1"`):
  https://github.com/playwright-community/playwright-go/blob/main/run.go
- [P1] Playwright, Installation (system requirements, what `npm init playwright` creates);
  release v1.65.0: https://playwright.dev/docs/intro,
  https://github.com/microsoft/playwright/releases
- [P2] Playwright, Web server: https://playwright.dev/docs/test-webserver
- [P3] Playwright, Best practices: https://playwright.dev/docs/best-practices
- [P4] Playwright, Browsers (channels, sizes, `--only-shell`): https://playwright.dev/docs/browsers
- [P5] Playwright, Keyboard: https://playwright.dev/docs/api/class-keyboard
- [P6] Playwright, Setting up CI: https://playwright.dev/docs/ci-intro
- [J1] jsdom README, unimplemented parts of the web platform: https://github.com/jsdom/jsdom
- [J2] jsdom v31.0.0 source tree, no `EventSource` implementation:
  https://github.com/jsdom/jsdom/tree/main/lib/jsdom/living
- [J3] jsdom `package.json` `engines`: https://github.com/jsdom/jsdom/blob/main/package.json
- [H1] happy-dom source: `Element.getBoundingClientRect`, `ResizeObserver`:
  https://github.com/capricorn86/happy-dom/tree/master/packages/happy-dom/src
- [N1] Node.js, Globals, `EventSource`: https://nodejs.org/api/globals.html
- [M1] MDN, `Event.isTrusted`: https://developer.mozilla.org/en-US/docs/Web/API/Event/isTrusted
- [T1] Testing Library, user-event introduction: https://testing-library.com/docs/user-event/intro
- [V1] Vitest 4 announcement (Browser Mode stable, provider packages):
  https://vitest.dev/blog/vitest-4
- [V2] Vitest `package.json` `engines`:
  https://github.com/vitest-dev/vitest/blob/main/packages/vitest/package.json
- [V3] Vitest, Browser Mode guide: https://vitest.dev/guide/browser/
