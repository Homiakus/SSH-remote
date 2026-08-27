#!/bin/bash
echo "=== Disk Usage Report ==="
df -h
echo ""
echo "=== Top directories by size ==="
du -sh /var/log/* 2>/dev/null | sort -rh | head -10
