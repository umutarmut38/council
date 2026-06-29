Output .tmp/vhs/council-vhs-cost-view.gif
Set Shell "bash"
Set FontSize 14
Set Width 1600
Set Height 900

Type "bash test/vhs/scenarios/cost-view.sh setup"
Enter
Wait+Screen@30s /READY cost-view/
Type "source .tmp/vhs/cost-view/env"
Enter
Type "clear"
Enter
Type "cd $WORK && $COUNCIL_BIN --agents priced,unknown ask 'show the cost view'"
Enter
Sleep 4s
Type "/cost"
Enter
Wait+Screen@30s /stale price/
Wait+Screen@30s /usage\.model is not configured/
Wait+Screen@30s /Confidence/
Sleep 1s
Ctrl+X
Sleep 1s
Type "bash $ROOT/test/vhs/scenarios/cost-view.sh assert"
Enter
Wait+Screen@30s /PASS cost-view/
Sleep 1s
