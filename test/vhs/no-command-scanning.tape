Output .tmp/vhs/council-vhs-no-command-scanning.gif

Require council

Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

# Configure the deterministic fixture in VHS, then show only the real council
# session. The opaque commands intentionally contain model selectors; council
# must launch them but never parse them for usage metadata.
Hide
Type "ROOT=$(pwd)"
Enter
Type "CASE_DIR=$ROOT/.tmp/vhs/no-command-scanning"
Enter
Type "HOME=$CASE_DIR/home"
Enter
Type "XDG_CONFIG_HOME=$CASE_DIR/xdg-config"
Enter
Type "XDG_DATA_HOME=$CASE_DIR/xdg-data"
Enter
Type "WORK=$CASE_DIR/work"
Enter
Type "BIN_DIR=$CASE_DIR/bin"
Enter
Type "export HOME XDG_CONFIG_HOME XDG_DATA_HOME"
Enter
Type "export PATH=$BIN_DIR:$ROOT/bin:$PATH"
Enter
Type "rm -rf $CASE_DIR && mkdir -p $HOME $XDG_CONFIG_HOME $XDG_DATA_HOME $WORK $BIN_DIR"
Enter
Type "printf '%s\n' '#!/usr/bin/env sh' 'echo fake-agent ready' 'while IFS= read -r line; do echo fake-agent received: $line; done' > $BIN_DIR/claude"
Enter
Type "cp $BIN_DIR/claude $BIN_DIR/codex && chmod +x $BIN_DIR/claude $BIN_DIR/codex"
Enter
Type "echo 'usage:' > $HOME/.council.yaml"
Enter
Type "echo '  enabled: true' >> $HOME/.council.yaml"
Enter
Type "echo '  estimator: bytes4' >> $HOME/.council.yaml"
Enter
Type "echo 'ui:' >> $HOME/.council.yaml"
Enter
Type "echo '  initial_prompt_delay_ms: 100' >> $HOME/.council.yaml"
Enter
Type "echo 'sessions:' >> $HOME/.council.yaml"
Enter
Type "echo '  root_dir: '$WORK/.council/runs >> $HOME/.council.yaml"
Enter
Type "echo 'agents:' >> $HOME/.council.yaml"
Enter
Type "echo '  claude:' >> $HOME/.council.yaml"
Enter
Type "echo '    enabled: true' >> $HOME/.council.yaml"
Enter
Type "echo '    command: ['$BIN_DIR/claude', --model, haiku]' >> $HOME/.council.yaml"
Enter
Type "echo '    cwd: '$WORK >> $HOME/.council.yaml"
Enter
Type "echo '  codex:' >> $HOME/.council.yaml"
Enter
Type "echo '    enabled: true' >> $HOME/.council.yaml"
Enter
Type "echo '    command: ['$BIN_DIR/codex', --model, gpt-5.4-mini]' >> $HOME/.council.yaml"
Enter
Type "echo '    cwd: '$WORK >> $HOME/.council.yaml"
Enter
Type "cd $WORK"
Enter
Type "clear"
Enter
Show

Type "council"
Enter
Wait+Screen@30s /Agents: claude, codex/
Sleep 2s
Type "opaque command check"
Enter
Wait+Screen@30s /sent to all agents/
Sleep 1s
Type "/cost"
Enter
Wait+Screen@30s /2 sessions: usage\.tool is not configured/
Wait+Screen@30s /2 sessions: usage\.model is not configured/
Wait+Screen@30s /2 sessions: price unknown for model --/
Screenshot .tmp/vhs/no-command-scanning-cost.png
Sleep 1s
