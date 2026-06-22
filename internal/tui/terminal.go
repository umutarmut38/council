package tui

// Terminal emulation for agent panes: the in-memory screen, escape/CSI/SGR
// handling, and the PTY delivery mechanics (send modes, submit sequences).

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/umutarmut38/council/internal/agent"
	"github.com/umutarmut38/council/internal/config"
)

func sendLine(session *agent.Session, message string) error {
	if session == nil {
		return nil
	}

	text, submit := splitLinePayload(session.Config.Terminal, message)

	// With no delay, send the whole payload in one write (the common case).
	// Some TUI agents run an async input loop and treat an Enter that arrives
	// in the same burst as the text as a literal newline rather than a submit;
	// for those, set terminal.submit_delay_ms so the submit lands on its own.
	delay := submitDelay(session.Config.Terminal)
	if submit == "" || delay <= 0 {
		return session.WriteString(text + submit)
	}

	if err := session.WriteString(text); err != nil {
		return err
	}
	go func() {
		time.Sleep(delay)
		_ = session.WriteString(submit)
	}()
	return nil
}

func splitLinePayload(terminal config.TerminalConfig, message string) (text string, submit string) {
	text = optionalSequence(terminal.BeforeSendSequence)
	switch strings.ToLower(strings.TrimSpace(terminal.SendMode)) {
	case "paste", "bracketed-paste":
		text += bracketedPaste(message)
	default:
		text += message
	}
	submit = submitSequence(terminal.SubmitSequence)
	submit += optionalSequence(terminal.AfterSubmitSequence)
	return text, submit
}

func linePayload(terminal config.TerminalConfig, message string) string {
	text, submit := splitLinePayload(terminal, message)
	return text + submit
}

func submitDelay(terminal config.TerminalConfig) time.Duration {
	if terminal.SubmitDelayMs <= 0 {
		return 0
	}
	return time.Duration(terminal.SubmitDelayMs) * time.Millisecond
}

func submitSequence(name string) string {
	return terminalSequence(name, "\r")
}

func optionalSequence(name string) string {
	return terminalSequence(name, "")
}

func terminalSequence(name string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "":
		return fallback
	case "none", "off", "false":
		return ""
	case "cr", "enter":
		return "\r"
	case "lf", "ctrl+j":
		return "\n"
	case "crlf":
		return "\r\n"
	case "esc", "escape":
		return "\x1b"
	case "ctrl+c":
		return "\x03"
	case "ctrl+d":
		return "\x04"
	case "ctrl+u", "clear-line", "kill-line":
		return "\x15"
	case "csi-enter", "kitty-enter":
		return "\x1b[13;1u"
	case "csi-enter-legacy", "kitty-enter-legacy":
		return "\x1b[13u"
	case "csi-ctrl-enter", "ctrl+enter", "kitty-ctrl-enter":
		return "\x1b[13;5u"
	case "csi-shift-enter", "shift+enter", "kitty-shift-enter":
		return "\x1b[13;2u"
	}
	if strings.HasPrefix(name, "raw:") {
		return strings.TrimPrefix(name, "raw:")
	}
	return fallback
}

func bracketedPaste(message string) string {
	clean := strings.ReplaceAll(message, "\x1b[200~", "")
	clean = strings.ReplaceAll(clean, "\x1b[201~", "")
	return "\x1b[200~" + clean + "\x1b[201~"
}

func (v *agentView) setScreenSize(width int, height int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	old := v.Screen
	v.Width = width
	v.Height = height
	v.Screen = make([][]screenCell, height)
	v.ScrollTop = 0
	v.ScrollBot = height - 1

	start := 0
	if len(old) > height {
		start = len(old) - height
	}
	for i := 0; i < height && start+i < len(old); i++ {
		v.Screen[i] = trimCells(old[start+i], width)
	}
	for i := range v.Screen {
		v.Screen[i] = trimCells(v.Screen[i], width)
	}
	v.clampCursor()
}

func (v *agentView) screenLines(height int, width int) []string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		return nil
	}
	if v.Width != width || v.Height != height {
		v.setScreenSize(width, height)
	}
	lines := make([]string, height)
	for i := 0; i < height; i++ {
		var cells []screenCell
		if i < len(v.Screen) {
			cells = trimCells(v.Screen[i], width)
		}
		cells = v.decorateDisplayCells(cells, width)
		lines[i] = renderCells(cells, width)
	}
	return lines
}

// screenLinesCursor renders like screenLines but overlays a block cursor
// (reverse video) at the emulator's cursor position, unless the program hid the
// cursor (DECTCEM ?25l). Used by the focused integrated-editor pane so the
// editor's cursor is visible — the plain panes don't draw one.
func (v *agentView) screenLinesCursor(height int, width int) []string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		return nil
	}
	if v.Width != width || v.Height != height {
		v.setScreenSize(width, height)
	}
	lines := make([]string, height)
	for i := 0; i < height; i++ {
		var cells []screenCell
		if i < len(v.Screen) {
			cells = trimCells(v.Screen[i], width)
		}
		cells = v.decorateDisplayCells(cells, width)
		if !v.CursorHidden && i == v.CursorRow && v.CursorCol >= 0 && v.CursorCol < width {
			cells = overlayCursor(cells, v.CursorCol, width)
		}
		lines[i] = renderCells(cells, width)
	}
	return lines
}

// overlayCursor returns cells padded to width with a reverse-video block at col,
// representing the text cursor (so it shows even past the line's last character).
func overlayCursor(cells []screenCell, col int, width int) []screenCell {
	out := make([]screenCell, width)
	for i := 0; i < width; i++ {
		if i < len(cells) {
			out[i] = cells[i]
		}
		if out[i].Ch == 0 {
			out[i].Ch = ' '
		}
	}
	out[col].SGR += "\x1b[7m"
	return out
}

func (v *agentView) decorateDisplayCells(cells []screenCell, width int) []screenCell {
	if width <= 0 || v.Session == nil || v.Session.Name != "codex" || !isCodexPromptRow(cells) {
		return cells
	}
	out := ensureCells(trimCells(cells, width), width)
	for i := range out {
		if out[i].Ch == 0 {
			out[i].Ch = ' '
		}
		if !hasBackgroundSGR(out[i].SGR) {
			out[i].SGR += "\x1b[48;5;235m"
		}
	}
	return out
}

func isCodexPromptRow(cells []screenCell) bool {
	text := strings.TrimLeft(cellsText(cells), " ")
	return strings.HasPrefix(text, "› ")
}

func cellsText(cells []screenCell) string {
	var b strings.Builder
	for _, cell := range cells {
		if cell.Ch == 0 {
			b.WriteRune(' ')
		} else {
			b.WriteRune(cell.Ch)
		}
	}
	return b.String()
}

func hasBackgroundSGR(sgr string) bool {
	if sgr == "" {
		return false
	}
	matches := csiPattern.FindAllString(sgr, -1)
	has := false
	for _, seq := range matches {
		if !strings.HasSuffix(seq, "m") {
			continue
		}
		params := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
		for _, code := range strings.Split(params, ";") {
			switch code {
			case "0", "49":
				has = false
			case "40", "41", "42", "43", "44", "45", "46", "47", "48", "100", "101", "102", "103", "104", "105", "106", "107":
				has = true
			}
		}
	}
	return has
}

func (v *agentView) applyTerminal(raw string) {
	if v.pending != "" {
		raw = v.pending + raw
		v.pending = ""
	}
	raw = oscPattern.ReplaceAllString(raw, "")
	for i := 0; i < len(raw); {
		b := raw[i]
		switch b {
		case '\x1b':
			next, complete := v.consumeEscape(raw, i)
			if !complete {
				// Sequence is cut off at the chunk boundary; stash the tail
				// and finish it when the next chunk arrives. Cap the buffer so
				// a malformed stream can't grow it without bound.
				if len(raw)-i <= 8192 {
					v.pending = raw[i:]
					return
				}
				i++
				continue
			}
			if next <= i {
				i++
			} else {
				i = next
			}
		case '\r':
			v.CursorCol = 0
			i++
		case '\n':
			v.newLine()
			i++
		case '\b':
			if v.CursorCol > 0 {
				v.CursorCol--
			}
			i++
		case '\t':
			spaces := 4 - (v.CursorCol % 4)
			for j := 0; j < spaces; j++ {
				v.putRune(' ')
			}
			i++
		default:
			r, size := utf8.DecodeRuneInString(raw[i:])
			if r == utf8.RuneError && size == 1 {
				i++
				continue
			}
			if r >= ' ' && r != '\x7f' {
				v.putRune(r)
			}
			i += size
		}
	}
}

// consumeEscape applies the escape sequence starting at index. It returns the
// index just past the sequence and whether the sequence was complete. When the
// sequence is cut off at the end of the buffer it returns complete=false so the
// caller can buffer the remainder for the next chunk.
func (v *agentView) consumeEscape(raw string, index int) (int, bool) {
	if index+1 >= len(raw) {
		return index, false
	}

	switch raw[index+1] {
	case '[':
		j := index + 2
		for j < len(raw) && !isCSIFinal(raw[j]) {
			j++
		}
		if j >= len(raw) {
			return index, false
		}
		v.handleCSI(raw[index+2:j], raw[j])
		return j + 1, true
	case ']':
		// OSC: a complete one was already stripped before the loop, so reaching
		// here means it is cut off at the buffer boundary.
		return index, false
	case 'P', 'X', '^', '_':
		// DCS/SOS/PM/APC string, terminated by ST (ESC \). Strip if complete,
		// otherwise buffer the remainder.
		end := strings.Index(raw[index:], "\x1b\\")
		if end < 0 {
			return index, false
		}
		return index + end + 2, true
	case 'c':
		v.clearScreen()
		return index + 2, true
	case '7':
		v.SavedRow = v.CursorRow
		v.SavedCol = v.CursorCol
		return index + 2, true
	case '8':
		v.CursorRow = v.SavedRow
		v.CursorCol = v.SavedCol
		v.clampCursor()
		return index + 2, true
	case ' ', '#', '%', '(', ')', '*', '+', '-', '.', '/':
		// Escape sequences carrying intermediate bytes (0x20-0x2F) before a
		// final byte (0x30-0x7E): charset designation (ESC ( B), 7/8-bit
		// controls (ESC SP F), DEC line size (ESC # 8), etc. We don't emulate
		// alternate charsets, but we must consume the WHOLE sequence (including
		// the final byte) so it isn't rendered as literal text — e.g. nvim's
		// frequent "ESC ( B" would otherwise leak a stray "B" onto the screen.
		j := index + 1
		for j < len(raw) && raw[j] >= 0x20 && raw[j] <= 0x2f {
			j++
		}
		if j >= len(raw) {
			return index, false // final byte not in this chunk yet; buffer it
		}
		return j + 1, true
	default:
		return index + 2, true
	}
}

func (v *agentView) handleCSI(rawParams string, command byte) {
	params := parseCSIParams(rawParams)
	first := paramDefault(params, 0, 1)

	switch command {
	case 'A':
		v.CursorRow -= first
	case 'B':
		v.CursorRow += first
	case 'C':
		v.CursorCol += first
	case 'D':
		v.CursorCol -= first
	case 'E':
		v.CursorRow += first
		v.CursorCol = 0
	case 'F':
		v.CursorRow -= first
		v.CursorCol = 0
	case 'G':
		v.CursorCol = first - 1
	case 'H', 'f':
		v.CursorRow = paramDefault(params, 0, 1) - 1
		v.CursorCol = paramDefault(params, 1, 1) - 1
	case 'J':
		v.eraseScreen(paramDefault(params, 0, 0))
	case 'K':
		v.eraseLine(paramDefault(params, 0, 0))
	case 'm':
		v.handleSGR(rawParams)
	case 'L':
		v.insertLines(first)
	case 'M':
		v.deleteLines(first)
	case 'P':
		v.deleteChars(first)
	case '@':
		v.insertBlanks(first)
	case 'S':
		v.scrollUp(first)
	case 'T':
		v.scrollDown(first)
	case 'd':
		v.CursorRow = first - 1
	case 'r':
		top := paramDefault(params, 0, 1) - 1
		bottom := paramDefault(params, 1, v.Height) - 1
		if top < 0 {
			top = 0
		}
		if bottom >= v.Height {
			bottom = v.Height - 1
		}
		if top < bottom {
			v.ScrollTop = top
			v.ScrollBot = bottom
			v.CursorRow = 0
			v.CursorCol = 0
		}
	case 'h', 'l':
		// DEC private modes (ESC[?<n>h sets, ESC[?<n>l resets). Parse exact mode
		// numbers so multi-param sequences like ESC[?1;25l are handled and modes
		// like ESC[?2500h do not false-match a substring.
		if strings.HasPrefix(rawParams, "?") {
			for _, mode := range parseCSIParams(rawParams) {
				switch mode {
				case 47, 1047, 1049:
					v.clearScreen()
				case 25:
					// DECTCEM: ?25h shows the cursor, ?25l hides it.
					v.CursorHidden = command == 'l'
				case 1000, 1002, 1003:
					// X11 mouse tracking modes: the child program opts into mouse
					// reporting. Only then do we forward mouse events to its PTY
					// (otherwise the SGR sequence would land as garbage at a prompt).
					// Note: ?1006/?1015 only select the report *encoding* and don't
					// themselves enable reporting, so they aren't tracked here.
					v.MouseModeOn = command == 'h'
				}
			}
		}
	case 's':
		v.SavedRow = v.CursorRow
		v.SavedCol = v.CursorCol
	case 'u':
		v.CursorRow = v.SavedRow
		v.CursorCol = v.SavedCol
	}
	v.clampCursor()
}

func (v *agentView) handleSGR(rawParams string) {
	rawParams = strings.TrimSpace(rawParams)
	if rawParams == "" {
		rawParams = "0"
	}

	parts := strings.Split(rawParams, ";")
	for _, part := range parts {
		if part == "0" {
			v.CurrentSGR = ""
			break
		}
	}
	if rawParams != "0" {
		v.CurrentSGR += "\x1b[" + rawParams + "m"
	}
	if len(v.CurrentSGR) > 512 {
		v.CurrentSGR = "\x1b[" + rawParams + "m"
	}
}

func (v *agentView) putRune(r rune) {
	if v.Height == 0 || v.Width == 0 {
		return
	}
	if v.CursorCol >= v.Width {
		v.newLine()
	}

	line := v.Screen[v.CursorRow]
	for len(line) < v.CursorCol {
		line = append(line, screenCell{Ch: ' '})
	}
	cell := screenCell{Ch: r, SGR: v.CurrentSGR}
	if v.CursorCol < len(line) {
		line[v.CursorCol] = cell
	} else {
		line = append(line, cell)
	}
	if len(line) > v.Width {
		line = line[:v.Width]
	}
	v.Screen[v.CursorRow] = line
	v.CursorCol++
}

func (v *agentView) addDisplayLine(line string) {
	v.CursorCol = 0
	for _, r := range line {
		v.putRune(r)
	}
	v.newLine()
}

func (v *agentView) newLine() {
	v.CursorCol = 0
	if v.CursorRow == v.ScrollBot {
		v.scrollRegionUp(v.ScrollTop, v.ScrollBot, 1)
		return
	}
	v.CursorRow++
	if v.CursorRow < v.Height {
		return
	}
	if v.Height <= 0 {
		v.CursorRow = 0
	} else {
		v.CursorRow = v.Height - 1
	}
}

func (v *agentView) clearScreen() {
	for i := range v.Screen {
		v.Screen[i] = nil
	}
	v.CursorRow = 0
	v.CursorCol = 0
	v.ScrollTop = 0
	v.ScrollBot = v.Height - 1
}

func (v *agentView) eraseScreen(mode int) {
	switch mode {
	case 0:
		v.eraseLine(0)
		for i := v.CursorRow + 1; i < len(v.Screen); i++ {
			v.Screen[i] = nil
		}
	case 1:
		for i := 0; i < v.CursorRow; i++ {
			v.Screen[i] = nil
		}
		v.eraseLine(1)
	case 2, 3:
		v.clearScreen()
	}
}

func (v *agentView) eraseLine(mode int) {
	if v.CursorRow < 0 || v.CursorRow >= len(v.Screen) {
		return
	}
	line := v.Screen[v.CursorRow]
	switch mode {
	case 0:
		if v.CursorCol < 0 {
			v.CursorCol = 0
		}
		if v.CursorCol > v.Width {
			v.CursorCol = v.Width
		}
		line = ensureCells(line, v.Width)
		for i := v.CursorCol; i < v.Width; i++ {
			line[i] = v.blankCell()
		}
	case 1:
		line = ensureCells(line, v.Width)
		end := v.CursorCol
		if end >= v.Width {
			end = v.Width - 1
		}
		for i := 0; i <= end && i < len(line); i++ {
			line[i] = v.blankCell()
		}
	case 2:
		line = make([]screenCell, v.Width)
		for i := range line {
			line[i] = v.blankCell()
		}
	}
	v.Screen[v.CursorRow] = line
}

func (v *agentView) insertLines(count int) {
	if count <= 0 || v.CursorRow < v.ScrollTop || v.CursorRow > v.ScrollBot {
		return
	}
	if count > v.ScrollBot-v.CursorRow+1 {
		count = v.ScrollBot - v.CursorRow + 1
	}
	for i := v.ScrollBot; i >= v.CursorRow+count; i-- {
		v.Screen[i] = v.Screen[i-count]
	}
	for i := 0; i < count; i++ {
		v.Screen[v.CursorRow+i] = nil
	}
}

func (v *agentView) deleteLines(count int) {
	if count <= 0 || v.CursorRow < v.ScrollTop || v.CursorRow > v.ScrollBot {
		return
	}
	if count > v.ScrollBot-v.CursorRow+1 {
		count = v.ScrollBot - v.CursorRow + 1
	}
	for i := v.CursorRow; i+count <= v.ScrollBot; i++ {
		v.Screen[i] = v.Screen[i+count]
	}
	for i := v.ScrollBot - count + 1; i <= v.ScrollBot; i++ {
		v.Screen[i] = nil
	}
}

func (v *agentView) insertBlanks(count int) {
	if count <= 0 || v.CursorRow < 0 || v.CursorRow >= v.Height {
		return
	}
	line := v.Screen[v.CursorRow]
	for len(line) < v.CursorCol {
		line = append(line, screenCell{Ch: ' '})
	}
	prefix := append([]screenCell(nil), line[:minInt(v.CursorCol, len(line))]...)
	blanks := make([]screenCell, count)
	for i := range blanks {
		blanks[i] = v.blankCell()
	}
	suffix := append(blanks, line[minInt(v.CursorCol, len(line)):]...)
	line = append(prefix, suffix...)
	if len(line) > v.Width {
		line = line[:v.Width]
	}
	v.Screen[v.CursorRow] = line
}

func (v *agentView) deleteChars(count int) {
	if count <= 0 || v.CursorRow < 0 || v.CursorRow >= v.Height {
		return
	}
	line := v.Screen[v.CursorRow]
	if v.CursorCol >= len(line) {
		return
	}
	end := v.CursorCol + count
	if end > len(line) {
		end = len(line)
	}
	line = append(line[:v.CursorCol], line[end:]...)
	v.Screen[v.CursorRow] = line
}

func (v *agentView) scrollUp(count int) {
	v.scrollRegionUp(v.ScrollTop, v.ScrollBot, count)
}

func (v *agentView) scrollRegionUp(top int, bottom int, count int) {
	if count <= 0 {
		return
	}
	if top < 0 {
		top = 0
	}
	if bottom >= v.Height {
		bottom = v.Height - 1
	}
	if top > bottom {
		return
	}
	size := bottom - top + 1
	if count > size {
		count = size
	}
	for i := top; i+count <= bottom; i++ {
		v.Screen[i] = v.Screen[i+count]
	}
	for i := bottom - count + 1; i <= bottom; i++ {
		v.Screen[i] = nil
	}
}

func (v *agentView) scrollDown(count int) {
	if count <= 0 {
		return
	}
	top := v.ScrollTop
	bottom := v.ScrollBot
	if top < 0 {
		top = 0
	}
	if bottom >= v.Height {
		bottom = v.Height - 1
	}
	if top > bottom {
		return
	}
	size := bottom - top + 1
	if count > size {
		count = size
	}
	for i := bottom; i >= top+count; i-- {
		v.Screen[i] = v.Screen[i-count]
	}
	for i := top; i < top+count; i++ {
		v.Screen[i] = nil
	}
}

func (v *agentView) clampCursor() {
	if v.Height <= 0 {
		v.CursorRow = 0
	} else {
		if v.CursorRow < 0 {
			v.CursorRow = 0
		}
		if v.CursorRow >= v.Height {
			v.CursorRow = v.Height - 1
		}
	}
	if v.ScrollTop < 0 {
		v.ScrollTop = 0
	}
	if v.ScrollBot <= 0 || v.ScrollBot >= v.Height {
		v.ScrollBot = v.Height - 1
	}
	if v.ScrollTop >= v.ScrollBot {
		v.ScrollTop = 0
		v.ScrollBot = v.Height - 1
	}
	if v.Width <= 0 {
		v.CursorCol = 0
	} else {
		if v.CursorCol < 0 {
			v.CursorCol = 0
		}
		if v.CursorCol >= v.Width {
			v.CursorCol = v.Width - 1
		}
	}
}

func parseCSIParams(raw string) []int {
	raw = strings.TrimLeft(raw, "?><")
	if raw == "" {
		return nil
	}
	fields := strings.Split(raw, ";")
	params := make([]int, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			params = append(params, 0)
			continue
		}
		value, err := strconv.Atoi(field)
		if err != nil {
			params = append(params, 0)
			continue
		}
		params = append(params, value)
	}
	return params
}

func paramDefault(params []int, index int, fallback int) int {
	if index >= len(params) || params[index] == 0 {
		return fallback
	}
	return params[index]
}

func isCSIFinal(b byte) bool {
	return b >= 0x40 && b <= 0x7e
}

func trimCells(cells []screenCell, width int) []screenCell {
	if width <= 0 || len(cells) == 0 {
		return nil
	}
	if len(cells) > width {
		cells = cells[:width]
	}
	out := make([]screenCell, len(cells))
	copy(out, cells)
	return out
}

func ensureCells(cells []screenCell, width int) []screenCell {
	if width <= 0 {
		return nil
	}
	if len(cells) >= width {
		return cells[:width]
	}
	out := make([]screenCell, width)
	copy(out, cells)
	for i := len(cells); i < width; i++ {
		out[i] = screenCell{Ch: ' '}
	}
	return out
}

func (v *agentView) blankCell() screenCell {
	return screenCell{Ch: ' ', SGR: v.CurrentSGR}
}

func renderCells(cells []screenCell, width int) string {
	if width <= 0 {
		return ""
	}

	var b strings.Builder
	active := ""
	for col := 0; col < width; col++ {
		cell := screenCell{Ch: ' '}
		if col < len(cells) {
			cell = cells[col]
			if cell.Ch == 0 {
				cell.Ch = ' '
			}
		}

		if cell.SGR != active {
			if active != "" {
				b.WriteString("\x1b[0m")
			}
			if cell.SGR != "" {
				b.WriteString(cell.SGR)
			}
			active = cell.SGR
		}
		b.WriteRune(cell.Ch)
	}
	if active != "" {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

func cleanTranscriptOutput(value string) string {
	value = oscPattern.ReplaceAllString(value, "")
	value = csiPattern.ReplaceAllString(value, "")
	value = controlPattern.ReplaceAllString(value, "")
	return value
}
