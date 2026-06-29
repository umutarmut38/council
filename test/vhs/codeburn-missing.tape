Output .tmp/vhs/council-vhs-codeburn-missing.gif
Set Shell "bash"
Set FontSize 14
Set Width 1600
Set Height 900

Type "bash test/vhs/scenarios/codeburn-missing.sh setup"
Enter
Wait+Screen@30s /READY codeburn-missing/
Type "source .tmp/vhs/codeburn-missing/env"
Enter
Type "clear"
Enter
Type "cd $WORK && PATH=$CASE_DIR/bin:/usr/bin:/bin $COUNCIL_BIN cost --source codeburn"
Enter
Wait+Screen@30s /codeburn is not installed/
Wait+Screen@30s /machine-wide totals/
Sleep 1s
Type "bash $ROOT/test/vhs/scenarios/codeburn-missing.sh assert"
Enter
Wait+Screen@30s /PASS codeburn-missing/
Sleep 1s
