# A codex pane reconciles from a fixture codex rollout: the reported model is the
# provider-session model (gpt-5.4-mini), never the --model flag in its command,
# and it prices via the bundled LiteLLM table. Setup + fixtures live in
# scenarios/reconciliation-codex.sh.
Output .tmp/vhs/council-vhs-reconciliation-codex.gif
Set Shell "bash"
Set FontSize 16
Set Width 1920
Set Height 1080
Set Padding 24
Set Theme "Dracula"
Set TypingSpeed 60ms
Set PlaybackSpeed 1.0

Type "cd $WORK && council cost 20260628-reconcile-codex"
Enter
Sleep 1s
# Final line; the gpt-5.4-mini reported row above is asserted by the scenario.
Wait+Screen@30s /litellm-bundled/
Screenshot .tmp/vhs/reconciliation-codex-cost.png
Sleep 1s
