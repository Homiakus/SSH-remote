#!/bin/bash
echo "=== Clearing old logs ==="
find /var/log -name "*.log" -mtime +30 -exec rm -f {} \;
echo "Old logs cleared"
echo ""
echo "=== Rotating current logs ==="
logrotate -f /etc/logrotate.conf 2>/dev/null || echo "logrotate not available"
echo "Done"
