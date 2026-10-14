#!/usr/bin/env bash
# Read-only device-access diagnostic. Never opens or reads input streams.
set -u
set -o pipefail
if [[ $# -ne 0 ]]; then
    echo 'Usage: bash tools/stage1-probe.sh' >&2
    exit 2
fi
for tool in timeout udevadm; do
    if ! command -v "$tool" >/dev/null; then
        echo "Probe unavailable: missing $tool" >&2
        exit 1
    fi
done
devices=0
readable=0
metadata_ok=true
for node in /dev/input/event*; do
    [[ -e $node ]] || continue
    if ! metadata=$(timeout 3s udevadm info --query=property --name="$node" 2>/dev/null); then
        metadata_ok=false
        continue
    fi
    if [[ $'\n'${metadata}$'\n' == *$'\nID_INPUT_KEYBOARD=1\n'* ]]; then
        devices=$((devices + 1))
        [[ -r $node ]] && readable=$((readable + 1))
    fi
done
printf 'keyboard_interfaces=%d readable=%d metadata_ok=%s\n' "$devices" "$readable" "$metadata_ok"
[[ $metadata_ok == true && $devices -gt 0 && $readable -eq $devices ]]
