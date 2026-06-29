Output .tmp/vhs/council-vhs-same-tool-same-cwd.gif
Set Shell "bash"
Set FontSize 14
Set Width 1600
Set Height 900

Type "bash test/vhs/scenarios/same-tool-same-cwd.sh setup"
Enter
Wait+Screen@30s /READY same-tool-same-cwd/
Type "source .tmp/vhs/same-tool-same-cwd/env"
Enter
Type "clear"
Enter
Type "cd $WORK && $COUNCIL_BIN cost 20260628-same-tool"
Enter
Wait+Screen@30s /claude-a/
Wait+Screen@30s /claude-b/
Wait+Screen@30s /litellm-bundled/
Sleep 1s
Type "bash $ROOT/test/vhs/scenarios/same-tool-same-cwd.sh assert"
Enter
Wait+Screen@30s /provider\.session/
Wait+Screen@30s /PASS same-tool-same-cwd/
Sleep 1s
