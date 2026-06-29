Output .tmp/vhs/council-vhs-no-command-scanning.gif
Set Shell "bash"
Set FontSize 14
Set Width 1600
Set Height 900

Type "bash test/vhs/scenarios/no-command-scanning.sh setup"
Enter
Wait+Screen@30s /READY no-command-scanning/
Type "source .tmp/vhs/no-command-scanning/env"
Enter
Type "clear"
Enter
Type "cd $WORK && $COUNCIL_BIN --agents fake ask 'opaque command check'"
Enter
Sleep 4s
Type "/cost"
Enter
Wait+Screen@30s /usage\.tool is not configured/
Wait+Screen@30s /usage\.model is not configured/
Sleep 1s
Ctrl+X
Sleep 1s
Type "bash $ROOT/test/vhs/scenarios/no-command-scanning.sh assert"
Enter
Wait+Screen@30s /PASS no-command-scanning/
Sleep 1s
