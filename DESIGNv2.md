# Omnideck TUI Design Specification

This document outlines the architecture and UI/UX design for the `omnideck` terminal interface, built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Design Philosophy

- **Unified Interface:** The TUI serves as a wrapper around the CLI. Every command is available via flags, but the TUI provides a structured environment for long-running operations (logs) and state management (config/status).
- **Keyboard-First:** Every action is mapped to a key binding.
- **Consistent Layout:** A persistent global header and footer frame the application, with a dynamic "content" area in the center.

---

## Layout Architecture

The TUI utilizes a standard `Bubble Tea` Model-View-Update (MVU) pattern with a layout splitter.

### Global Components

1.  **Header (Top):** Displays tool logo, current context/instance name, and status (e.g., "Omnideck: Running").
2.  **Main Content (Center):** Dynamic container for the active screen.
3.  **Footer (Bottom):** Persistent keyboard hints (e.g., `q: Quit | j/k: Nav | enter: Select | l: Logs`). Hints update contextually based on the active screen.

---

## Screen Definitions

### 1. Dashboard (Main Screen)
The default landing view.
- **Content:** A table view showing all managed instances (Name, Port, Status, CPU/RAM usage).
- **Actions:**
    - `j/k`: Navigate instances.
    - `enter`: Select instance (opens Instance Detail).
    - `n`: New instance (launches `install` wizard).
    - `d`: Run `doctor` (opens modal).
    - `q`: Quit.

### 2. Instance Detail
Opened when an instance is selected.
- **Content:** Split view or vertical stack showing:
    - **Metadata:** Port, uptime, image version.
    - **Logs (Preview):** A scrollable viewport tailing logs.
- **Actions:**
    - `s`: Stop/Start container.
    - `l`: Enter "Full Logs" mode.
    - `c`: Open "Config Edit" mode.
    - `backspace`: Return to Dashboard.

### 3. Config Edit (Modal/Overlay)
- **Content:** A form-based interface using `bubbles/textinput`.
- **Flow:** Displays current configuration keys. Navigating to a key and hitting `enter` allows editing. Changes are saved to `~/.config/omnideck-cli/instances/<name>.yaml`.
- **Actions:**
    - `esc`: Cancel changes.
    - `ctrl+s`: Save and exit to Detail view.

### 4. Full Logs (Viewport)
- **Content:** Uses `bubbles/viewport` to display raw logs.
- **Actions:**
    - `g/G`: Top/Bottom of logs.
    - `ctrl+c`: Stop TUI.
    - `backspace`: Return to Detail view.

---

## Technical Considerations

### Component Library
- **List:** For the Dashboard (instance management).
- **Table:** For showing system/config details.
- **Viewport:** For log tailing and rendering.
- **Textinput/Form:** For config editing and the install wizard.
- **Spinner:** For long-running operations (install/restart).

### Handling "Non-TUI" Execution
The CLI architecture ensures that logic is decoupled from the `View` function:
1.  **Core Package:** All CLI logic (the "what") lives in a `pkg/omnideck` library.
2.  **CLI Runner:** `main.go` parses `os.Args` and calls the core library.
3.  **TUI Runner:** If no commands are provided or `-tui` is flag-set, `main.go` initializes the Bubble Tea model.

### Communication Pattern
- The TUI uses `tea.Cmd` to execute commands in a separate goroutine.
- Output from the core package (e.g., log streams) is sent to the Model via `tea.Msg` to ensure thread-safe updates to the screen.

---

## Key Mapping Convention

| Key | Context | Action |
|---|---|---|
| `q` | Global | Quit application |
| `j`/`k` | List/Table | Scroll down/up |
| `enter` | List | Select / Enter Detail |
| `l` | Detail View | Open logs |
| `c` | Detail View | Edit config |
| `s` | Detail View | Toggle Start/Stop |
| `esc`/`backspace` | Modal/Sub-screen | Go back |
