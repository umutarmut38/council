Output .tmp/vhs/council-vhs-estimated-cost.gif
Set Shell "bash"
Set FontSize 14
Set Width 1600
Set Height 900

Type "bash test/vhs/scenarios/estimated-cost.sh setup"
Enter
Wait+Screen@30s /READY estimated-cost/
Type "source .tmp/vhs/estimated-cost/env"
Enter
Type "clear"
Enter
Type "cd $WORK && $COUNCIL_BIN --agents fake ask 'estimate a priced prompt'"
Enter
Sleep 4s
Type "/cost"
Enter
Wait+Screen@30s /gpt-5/
Wait+Screen@30s /litellm-bundled/
Sleep 1s
Ctrl+X
Sleep 1s
Type "bash $ROOT/test/vhs/scenarios/estimated-cost.sh assert"
Enter
Wait+Screen@30s /PASS estimated-cost/
Sleep 1s
