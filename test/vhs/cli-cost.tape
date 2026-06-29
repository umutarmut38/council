Output .tmp/vhs/council-vhs-cli-cost.gif
Set Shell "bash"
Set FontSize 14
Set Width 1600
Set Height 900

Type "bash test/vhs/scenarios/cli-cost.sh setup"
Enter
Wait+Screen@30s /READY cli-cost/
Type "source .tmp/vhs/cli-cost/env"
Enter
Type "clear"
Enter
Type "cd $WORK && $COUNCIL_BIN cost 20260628-cli-cost"
Enter
Wait+Screen@30s /local-free/
Wait+Screen@30s /\$0\.00/
Sleep 1s
Type "bash $ROOT/test/vhs/scenarios/cli-cost.sh assert"
Enter
Wait+Screen@30s /PASS cli-cost/
Sleep 1s
