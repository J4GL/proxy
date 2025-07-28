let ws;
let reconnectInterval;
let bandwidthChart;
let expandedDomains = {}; // Track expanded domain states
let bandwidthData = {
    labels: [],
    datasets: [{
        label: 'Download (Mo/s)',
        data: [],
        borderColor: '#4CAF50',
        backgroundColor: 'rgba(76, 175, 80, 0.1)',
        fill: true,
        tension: 0.4
    }, {
        label: 'Upload (Mo/s)', 
        data: [],
        borderColor: '#2196F3',
        backgroundColor: 'rgba(33, 150, 243, 0.1)',
        fill: true,
        tension: 0.4
    }]
};

function initBandwidthChart() {
    const ctx = document.getElementById('bandwidth-chart').getContext('2d');
    bandwidthChart = new Chart(ctx, {
        type: 'line',
        data: bandwidthData,
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: {
                    position: 'top',
                },
                title: {
                    display: true,
                    text: 'Bandwidth Usage Over Time'
                }
            },
            scales: {
                x: {
                    display: true,
                    title: {
                        display: true,
                        text: 'Time'
                    }
                },
                y: {
                    display: true,
                    title: {
                        display: true,
                        text: 'Bandwidth (Mo/s)'
                    },
                    beginAtZero: true
                }
            },
            animation: {
                duration: 0
            },
            elements: {
                point: {
                    radius: 0
                }
            }
        }
    });
}

function formatBytes(bytes) {
    if (bytes === 0) return '0 KB/s';
    
    const gb = bytes / (1024 * 1024 * 1024);
    if (gb >= 1) {
        return parseFloat(gb.toFixed(2)) + ' GB/s';
    }
    
    const mb = bytes / (1024 * 1024);
    if (mb >= 1) {
        return parseFloat(mb.toFixed(2)) + ' MB/s';
    }
    
    const kb = bytes / 1024;
    return parseFloat(kb.toFixed(2)) + ' KB/s';
}

function updateBandwidthChart(bandwidthIn, bandwidthOut) {
    const now = new Date();
    const timeLabel = now.toLocaleTimeString();
    
    // Convert bytes/s to MB/s
    const downloadMBs = bandwidthIn / (1024 * 1024);
    const uploadMBs = bandwidthOut / (1024 * 1024);
    
    // Keep only last 50 data points
    if (bandwidthData.labels.length >= 50) {
        bandwidthData.labels.shift();
        bandwidthData.datasets[0].data.shift();
        bandwidthData.datasets[1].data.shift();
    }
    
    bandwidthData.labels.push(timeLabel);
    bandwidthData.datasets[0].data.push(downloadMBs);
    bandwidthData.datasets[1].data.push(uploadMBs);
    
    bandwidthChart.update('none');
}

function connectWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = protocol + '//' + window.location.host + '/ws';

    ws = new WebSocket(wsUrl);

    ws.onopen = function() {
        console.log('WebSocket connected');
        document.getElementById('connection-status').innerHTML = '🟢 Connected to monitoring server';
        document.getElementById('connection-status').style.background = '#e8f5e8';
        document.getElementById('connection-status').style.borderColor = '#4caf50';
        if (reconnectInterval) {
            clearInterval(reconnectInterval);
            reconnectInterval = null;
        }
    };

    ws.onmessage = function(event) {
        const data = JSON.parse(event.data);
        updateDashboard(data);
    };

    ws.onclose = function() {
        console.log('WebSocket disconnected');
        document.getElementById('connection-status').innerHTML = '🔴 Disconnected from monitoring server';
        document.getElementById('connection-status').style.background = '#ffebee';
        document.getElementById('connection-status').style.borderColor = '#f44336';

        // Attempt to reconnect every 5 seconds
        if (!reconnectInterval) {
            reconnectInterval = setInterval(connectWebSocket, 5000);
        }
    };

    ws.onerror = function(error) {
        console.error('WebSocket error:', error);
    };
}

function toggleDomain(domainId) {
    const subdomainRows = document.querySelectorAll('.' + domainId);
    const domainRow = document.querySelector('.domain-group[onclick*="' + domainId + '"]');
    const toggle = domainRow.querySelector('.domain-toggle');
    
    const isExpanded = expandedDomains[domainId] || false;
    
    if (isExpanded) {
        // Collapse
        subdomainRows.forEach(row => row.style.display = 'none');
        domainRow.classList.remove('expanded');
        toggle.innerHTML = toggle.innerHTML.replace('▼', '▶');
        expandedDomains[domainId] = false;
    } else {
        // Expand
        subdomainRows.forEach(row => row.style.display = 'table-row');
        domainRow.classList.add('expanded');
        toggle.innerHTML = toggle.innerHTML.replace('▶', '▼');
        expandedDomains[domainId] = true;
    }
}

function restoreExpandedDomains() {
    // Restore expanded states after DOM update
    for (const domainId in expandedDomains) {
        if (expandedDomains[domainId]) {
            const subdomainRows = document.querySelectorAll('.' + domainId);
            const domainRow = document.querySelector('.domain-group[onclick*="' + domainId + '"]');
            if (domainRow) {
                const toggle = domainRow.querySelector('.domain-toggle');
                subdomainRows.forEach(row => row.style.display = 'table-row');
                domainRow.classList.add('expanded');
                if (toggle && toggle.innerHTML.includes('▶')) {
                    toggle.innerHTML = toggle.innerHTML.replace('▶', '▼');
                }
            }
        }
    }
}

function formatNumber(num) {
    if (num >= 1000000) {
        return (num / 1000000).toFixed(1) + 'M';
    } else if (num >= 1000) {
        return (num / 1000).toFixed(1) + 'K';
    }
    return num.toString();
}

function updateDashboard(data) {
    document.getElementById('total-connections').textContent = formatNumber(data.total_connections || 0);
    document.getElementById('active-connections').textContent = formatNumber(Object.keys(data.active_connections || {}).length);
    
    // Update bandwidth display
    const bandwidthIn = data.current_bandwidth_in || 0;
    const bandwidthOut = data.current_bandwidth_out || 0;
    document.getElementById('bandwidth-in').textContent = formatBytes(bandwidthIn);
    document.getElementById('bandwidth-out').textContent = formatBytes(bandwidthOut);
    
    // Update bandwidth chart
    if (bandwidthChart) {
        updateBandwidthChart(bandwidthIn, bandwidthOut);
    }

    const connectionsContent = document.getElementById('connections-content');
    const connections = data.active_connections || {};

    if (Object.keys(connections).length === 0) {
        connectionsContent.innerHTML = '<div class="no-connections">No active connections</div>';
    } else {
        // Group connections by domain first, then by subdomain
        const domainGroups = {};
        
        Object.values(connections).forEach(conn => {
            const fullDomain = conn.domain_name && conn.domain_name !== conn.destination.split(':')[0] 
                ? conn.domain_name 
                : conn.destination.split(':')[0];
            
            // Extract main domain (e.g., google.com from mail.google.com)
            let mainDomain = fullDomain;
            if (fullDomain && fullDomain !== 'N/A' && fullDomain.includes('.')) {
                const parts = fullDomain.split('.');
                if (parts.length >= 2) {
                    mainDomain = '*.' + parts.slice(-2).join('.');
                }
            }
            
            if (!domainGroups[mainDomain]) {
                domainGroups[mainDomain] = {
                    subdomains: {},
                    totalCount: 0,
                    allProtocols: new Set(),
                    allClientIps: new Set(),
                    earliestStart: conn.start_time
                };
            }
            
            // Group by full destination (subdomain + port)
            const subdomainKey = conn.destination;
            if (!domainGroups[mainDomain].subdomains[subdomainKey]) {
                domainGroups[mainDomain].subdomains[subdomainKey] = {
                    destination: conn.destination,
                    fullDomain: fullDomain,
                    count: 0,
                    protocols: new Set(),
                    client_ips: new Set(),
                    earliest_start: conn.start_time
                };
            }
            
            domainGroups[mainDomain].subdomains[subdomainKey].count++;
            domainGroups[mainDomain].subdomains[subdomainKey].protocols.add(conn.protocol);
            domainGroups[mainDomain].subdomains[subdomainKey].client_ips.add(conn.client_ip);
            
            domainGroups[mainDomain].totalCount++;
            domainGroups[mainDomain].allProtocols.add(conn.protocol);
            domainGroups[mainDomain].allClientIps.add(conn.client_ip);
            
            if (conn.start_time < domainGroups[mainDomain].earliestStart) {
                domainGroups[mainDomain].earliestStart = conn.start_time;
            }
            if (conn.start_time < domainGroups[mainDomain].subdomains[subdomainKey].earliest_start) {
                domainGroups[mainDomain].subdomains[subdomainKey].earliest_start = conn.start_time;
            }
        });
        
        let tableHTML = '<table><thead><tr><th>Client IPs</th><th>Protocol</th><th>Destination/Domain</th><th>Count</th><th>First Connection</th></tr></thead><tbody>';
        
        // Sort domains by total connection count (descending)
        const sortedDomains = Object.entries(domainGroups).sort((a, b) => b[1].totalCount - a[1].totalCount);
        
        sortedDomains.forEach(([domain, group]) => {
            const subdomainCount = Object.keys(group.subdomains).length;
            
            // Only create domain groups if there are multiple subdomains
            if (subdomainCount <= 1) {
                // Single subdomain - display directly without grouping
                Object.entries(group.subdomains).forEach(([subKey, subdomain]) => {
                    const subProtocolList = Array.from(subdomain.protocols).map(p => 
                        '<span class="protocol-badge protocol-' + p.toLowerCase() + '">' + p + '</span>'
                    ).join(' ');
                    
                    const subStartTime = new Date(subdomain.earliest_start).toLocaleString();
                    const subClientIpsList = Array.from(subdomain.client_ips).join(', ');
                    const displayDomain = subdomain.fullDomain === 'N/A' ? '<em>' + subdomain.destination + '</em>' : subdomain.fullDomain;
                    
                    tableHTML += '<tr>' +
                        '<td>' + subClientIpsList + '</td>' +
                        '<td>' + subProtocolList + '</td>' +
                        '<td>' + displayDomain + '</td>' +
                        '<td>' + subdomain.count + '</td>' +
                        '<td>' + subStartTime + '</td>' +
                        '</tr>';
                });
                return; // Skip the grouped domain logic
            }
            const protocolList = Array.from(group.allProtocols).map(p => {
                const protocolClass = p.toLowerCase() === 'http' ? 'protocol-http' : 'protocol-socks5';
                return '<span class="protocol-badge ' + protocolClass + '">' + p + '</span>';
            }).join(' ');
            
            const startTime = new Date(group.earliestStart).toLocaleString();
            const clientIpsList = Array.from(group.allClientIps).join(', ');
            const domainId = 'domain-' + domain.replace(/[^a-zA-Z0-9]/g, '-');
            
            // Domain group row (clickable)
            tableHTML += '<tr class="domain-group" onclick="toggleDomain(\'' + domainId + '\')">' +
                '<td>' + clientIpsList + '</td>' +
                '<td>' + protocolList + '</td>' +
                '<td><span class="domain-toggle">▶ ' + domain + '</span></td>' +
                '<td><strong>' + group.totalCount + '</strong></td>' +
                '<td>' + startTime + '</td>' +
                '</tr>';
            
            // Subdomain rows (initially hidden)
            const sortedSubdomains = Object.entries(group.subdomains).sort((a, b) => b[1].count - a[1].count);
            sortedSubdomains.forEach(([subdomainKey, subdomain]) => {
                const subProtocolList = Array.from(subdomain.protocols).map(p => {
                    const protocolClass = p.toLowerCase() === 'http' ? 'protocol-http' : 'protocol-socks5';
                    return '<span class="protocol-badge ' + protocolClass + '">' + p + '</span>';
                }).join(' ');
                
                const subStartTime = new Date(subdomain.earliest_start).toLocaleString();
                const subClientIpsList = Array.from(subdomain.client_ips).join(', ');
                const displayDomain = subdomain.fullDomain === 'N/A' ? '<em>' + subdomain.destination + '</em>' : subdomain.fullDomain;
                
                tableHTML += '<tr class="subdomain-row ' + domainId + '" style="display: none;">' +
                    '<td>' + subClientIpsList + '</td>' +
                    '<td>' + subProtocolList + '</td>' +
                    '<td>' + displayDomain + '</td>' +
                    '<td>' + subdomain.count + '</td>' +
                    '<td>' + subStartTime + '</td>' +
                    '</tr>';
            });
        });

        tableHTML += '</tbody></table>';
        connectionsContent.innerHTML = tableHTML;
        // Restore expanded domain states after DOM update
        restoreExpandedDomains();
    }

    document.getElementById('last-updated').textContent = 'Last updated: ' + new Date().toLocaleString();
}

// Initialize bandwidth chart
initBandwidthChart();

// Initialize WebSocket connection
connectWebSocket();

// Fallback: fetch data every 30 seconds if WebSocket fails
setInterval(function() {
    if (ws.readyState !== WebSocket.OPEN) {
        fetch('/api/stats')
            .then(response => response.json())
            .then(data => updateDashboard(data))
            .catch(error => console.error('Error fetching data:', error));
    }
}, 30000);