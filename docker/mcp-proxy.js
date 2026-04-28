#!/usr/bin/env node
// MCP stdio proxy — fixes params.arguments format for PicoClaw compatibility
// Usage: node mcp-proxy.js <real-command> <real-args...>
// PicoClaw sends arguments as null/undefined for some tool calls,
// but chrome-devtools-mcp expects a record (object). This proxy fixes that.

const { spawn } = require('child_process');
const readline = require('readline');

if (process.argv.length < 3) {
  process.stderr.write('Usage: node mcp-proxy.js <command> [args...]\n');
  process.exit(1);
}

const child = spawn(process.argv[2], process.argv.slice(3), {
  stdio: ['pipe', 'pipe', 'inherit'],
  env: process.env,
});

function fixMessage(line) {
  try {
    const msg = JSON.parse(line);
    // Fix tools/call: ensure params.arguments is always an object
    if (msg.method === 'tools/call' && msg.params) {
      const args = msg.params.arguments;
      if (args === null || args === undefined ||
          typeof args !== 'object' || Array.isArray(args)) {
        msg.params.arguments = {};
      }
      return JSON.stringify(msg);
    }
  } catch {}
  return line;
}

// PicoClaw → proxy → chrome-devtools-mcp
const input = readline.createInterface({ input: process.stdin, terminal: false });
input.on('line', (line) => {
  child.stdin.write(fixMessage(line) + '\n');
});
input.on('close', () => { try { child.stdin.end(); } catch {} });

// chrome-devtools-mcp → proxy → PicoClaw
const output = readline.createInterface({ input: child.stdout, terminal: false });
output.on('line', (line) => {
  process.stdout.write(line + '\n');
});

child.on('exit', (code) => process.exit(code || 0));
process.on('SIGTERM', () => child.kill('SIGTERM'));
process.on('SIGINT', () => child.kill('SIGINT'));
