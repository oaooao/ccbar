#!/bin/bash
# Demo script for generating screenshots
# Usage: bash scripts/demo.sh

CCBAR="./ccbar"

echo ""
echo "━━━ Normal (low usage) ━━━"
echo ""
echo '{"model":{"id":"claude-opus-4-6","display_name":"Opus 4.6 (1M context)"},"cwd":"/Users/oao/Code/projects/cli/ccbar","session_id":"demo-1","workspace":{"current_dir":"/Users/oao/Code/projects/cli/ccbar","project_dir":"/Users/oao/Code/projects/cli/ccbar","added_dirs":[]},"version":"2.1.90","cost":{"total_cost_usd":0.42,"total_duration_ms":420000},"context_window":{"context_window_size":1000000,"used_percentage":18},"rate_limits":{"five_hour":{"used_percentage":12,"resets_at":'"$(date -v+4H +%s)"'},"seven_day":{"used_percentage":8,"resets_at":'"$(date -v+6d +%s)"'}}}' | $CCBAR

echo ""
echo "━━━ Warning (moderate usage) ━━━"
echo ""
echo '{"model":{"id":"claude-opus-4-6","display_name":"Opus 4.6 (1M context)"},"cwd":"/Users/oao/Code/projects/cli/ccbar","session_id":"demo-2","workspace":{"current_dir":"/Users/oao/Code/projects/cli/ccbar","project_dir":"/Users/oao/Code/projects/cli/ccbar","added_dirs":[]},"version":"2.1.90","cost":{"total_cost_usd":3.17,"total_duration_ms":2040000},"context_window":{"context_window_size":1000000,"used_percentage":72},"rate_limits":{"five_hour":{"used_percentage":65,"resets_at":'"$(date -v+2H +%s)"'},"seven_day":{"used_percentage":71,"resets_at":'"$(date -v+3d +%s)"'}}}' | $CCBAR

echo ""
echo "━━━ Critical (high usage, non-default model) ━━━"
echo ""
echo '{"model":{"id":"claude-sonnet-4-6","display_name":"Sonnet 4.6"},"cwd":"/Users/oao/Code/projects/cli/ccbar","session_id":"demo-3","workspace":{"current_dir":"/Users/oao/Code/projects/cli/ccbar","project_dir":"/Users/oao/Code/projects/cli/ccbar","added_dirs":[]},"version":"2.1.90","cost":{"total_cost_usd":8.52,"total_duration_ms":4200000},"context_window":{"context_window_size":200000,"used_percentage":93},"rate_limits":{"five_hour":{"used_percentage":91,"resets_at":'"$(date -v+1H +%s)"'},"seven_day":{"used_percentage":85,"resets_at":'"$(date -v+2d +%s)"'}}}' | $CCBAR

echo ""
