Output .tmp/vhs/council-vhs-reconciliation-claude.gif
Set Shell "bash"
Set FontSize 14
Set Width 1600
Set Height 900

Type "bash test/vhs/scenarios/reconciliation-claude.sh setup"
Enter
Wait+Screen@30s /READY reconciliation-claude/
Type "source .tmp/vhs/reconciliation-claude/env"
Enter
Type "clear"
Enter
Type "cd $WORK && $COUNCIL_BIN cost 20260628-reconcile"
Enter
Wait+Screen@30s /claude-sonnet-4-6/
Wait+Screen@30s /litellm-bundled/
Sleep 1s
Type "bash $ROOT/test/vhs/scenarios/reconciliation-claude.sh assert"
Enter
Wait+Screen@30s /provider\.session/
Wait+Screen@30s /PASS reconciliation-claude/
Sleep 1s
