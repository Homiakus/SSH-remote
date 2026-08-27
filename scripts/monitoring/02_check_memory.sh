#!/bin/bash
echo "=== Memory Usage ==="
free -h
echo ""
echo "=== Top processes by memory ==="
ps aux --sort=-%mem | head -10
