#!/usr/bin/env node
import readline from 'node:readline';
import { statSync } from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { PassThrough } from 'node:stream';
import { printers, links, networks, printerModes, searchPrinters, validateUsername } from '../src/model.mjs';

const commands = ['/print', '/printers', '/jobs', '/cancel', '/account', '/network', '/help', '/quit'];
const help = `Print @ SoC · interactive design prototype

Usage: node bin/socprint.mjs --demo
       npm start

Arrow keys choose · Enter continues · Esc goes home · Ctrl+C exits
Click Print, Printers, Jobs or Help, or choose with arrow keys and Enter.

This prototype simulates login, connections, uploads and print jobs.
It never reads a password, connects to SoC or sends a real print job.
Local PDF paths can be selected; their contents are not read or uploaded.
`;
if (process.argv.includes('--help') || process.argv.includes('-h')) { console.log(help); process.exit(0); }
if (process.argv.slice(2).some(arg => arg !== '--demo')) { console.error(help); process.exit(1); }
if (!process.stdin.isTTY || !process.stdout.isTTY) { console.log(help); process.exit(0); }

const theme = { accent: '38;5;116', warm: '38;5;215', text: '39', muted: '90', good: '32', bad: '31', selected: '7' };
const clean = value => String(value).replace(/[\x00-\x1f\x7f-\x9f]/g, '');
const ink = (name, value) => `\x1b[${theme[name]}m${clean(value)}\x1b[0m`;
const plain = value => value.replace(/\x1b\[[0-9;]*m/g, '');
const state = {
  screen: 'home', input: '', selected: 0, username: '', network: networks[1],
  file: null, printer: null, mode: null, flow: false, showRestricted: false,
  notice: '', error: '', progress: 0, jobs: [], timer: null, stopped: false,
};
let clickTargets = [];

function width() { return Math.max(24, Math.min(process.stdout.columns || 90, 100) - 4); }
function short(value, n = width()) { const chars = [...clean(value)]; return chars.length > n ? chars.slice(0, Math.max(0, n - 1)).join('') + '…' : chars.join(''); }
function wrap(value, n = width()) {
  const words = clean(value).split(' '); const lines = []; let line = '';
  for (const word of words) {
    if ((line + ' ' + word).trim().length > n && line) { lines.push(line); line = ''; }
    if (word.length > n) { if (line) { lines.push(line); line = ''; } for (let i = 0; i < word.length; i += n) lines.push(word.slice(i, i + n)); }
    else line += (line ? ' ' : '') + word;
  }
  if (line) lines.push(line); return lines;
}
function go(screen, options = {}) { Object.assign(state, { screen, input: '', selected: 0, error: '' }, options); render(); }
function detail(label, value) { return ink('muted', `${(label + ':').padEnd(11)} `) + short(value, width() - 12); }
function box(lines) {
  const w = width(); const out = [ink('accent', '╭' + '─'.repeat(w - 2) + '╮')];
  for (const line of lines) {
    const content = plain(line).length > w - 4 ? short(plain(line), w - 4) : line;
    out.push(ink('accent', '│') + ' ' + content + ' '.repeat(Math.max(0, w - 4 - plain(content).length)) + ' ' + ink('accent', '│'));
  }
  out.push(ink('accent', '╰' + '─'.repeat(w - 2) + '╯')); return out;
}
function options() {
  switch (state.screen) {
    case 'home': return ['Print', 'Printers', 'Jobs', 'Help'].map(label => ({ label, value: '/' + label.toLowerCase() }));
    case 'network': return networks.map(n => ({ label: n.label, detail: n.detail, value: n }));
    case 'printer': case 'printers': return searchPrinters(state.input, state.showRestricted).map(p => ({ label: `${p.id} · ${p.paper} · ${p.kind}`, detail: p.location, value: p, disabled: p.access !== 'public' }));
    case 'mode': return printerModes(state.printer).map(m => ({ label: m.label, detail: m.queue, value: m }));
    case 'review': return [{ label: 'Print demo job', detail: 'Confirm this file and this exact queue', value: 'print' }, { label: 'Change printer', value: 'printer' }, { label: 'Cancel', value: 'cancel' }];
    case 'cancel': return state.jobs.filter(j => !j.cancelled).map(j => ({ label: `${j.id} · ${j.file.name}`, detail: `${j.mode.queue} · simulated`, value: j }));
    default: return [];
  }
}
function menu(items, lines, limit = 5, showDetail = true) {
  state.selected = Math.min(state.selected, Math.max(0, items.length - 1));
  const start = Math.max(0, state.selected - limit + 1);
  for (let i = start; i < Math.min(items.length, start + limit); i++) {
    const item = items[i], active = i === state.selected;
    const label = short(item.label + (item.disabled ? ' · restricted' : ''), width() - 3);
    clickTargets.push({ y: lines.length + 1, x: 3, end: Math.min(width() + 2, label.length + 6), action: () => { state.selected = i; enter(); } });
    lines.push((active ? ink('accent', '❯ ') : '  ') + ink(item.disabled ? 'muted' : active ? 'accent' : 'text', label));
    if (item.detail && showDetail) lines.push('  ' + ink('muted', short(item.detail, width() - 2)));
  }
  if (!items.length) lines.push(ink('muted', '  No matching choices.'));
  if (items.length > limit) lines.push(ink('muted', `  ${state.selected + 1} / ${items.length} · scroll with ↑ ↓`));
}
function render() {
  clickTargets = [];
  const lines = [ink('accent', '◇ Print @ SoC') + '  ' + ink('muted', 'v0.1 · DEMO'), ''];
  const rows = Math.max(20, process.stdout.rows || 32);
  switch (state.screen) {
    case 'home': {
      clickTargets.push({ y: lines.length + 1, x: 3, end: width() + 2, action: () => runCommand('/account') });
      lines.push(detail('Account', state.username || 'Not set'));
      clickTargets.push({ y: lines.length + 1, x: 3, end: width() + 2, action: () => runCommand('/network') });
      lines.push(detail('Network', state.network.label), '');
      menu(options().map(item => ({ ...item, label: `[ ${item.label} ]` })), lines, 4, false);
      if (state.notice) lines.push('', ...wrap(state.notice).map(s => ink('good', s)));
      if (state.input.startsWith('/')) {
        const matches = commands.filter(c => c.startsWith(state.input));
        if (matches.length) lines.push('', ink('muted', short(matches.join('   '))), ink('muted', 'Tab completes the first match'));
      }
      if (state.input) lines.push('', ink('accent', '❯ ') + short(state.input, width() - 3) + ink('muted', '▏'));
      break;
    }
    case 'account':
      lines.push(ink('warm', '01  Your SoC account'), '', ...wrap('Enter your SoC Unix username to set up this demo.').map(s => ink('text', s)), '', ink('accent', '❯ ') + short(state.input, width() - 3) + ink('muted', '▏'), '');
      for (const [label, url] of [['Create an account', links.account], ['Enable Unix server access', links.services], ['Set up SSH keys', links.keys]]) lines.push(ink('muted', label), ...wrap(url).map(s => ink('accent', s)), '');
      break;
    case 'network':
      lines.push(ink('warm', '02  Network'), '');
      menu(options(), lines, 3, false);
      if (state.selected === 2) lines.push('', ink('warm', 'Connect to NUS VPN before using the live app.'));
      break;
    case 'file':
      lines.push(ink('warm', '03  Choose your PDF'), '', 'Paste a path or drag a PDF into this terminal.', ink('muted', 'Enter with an empty path uses the example CS1231S.pdf.'), '', ink('accent', '❯ ') + short(state.input, width() - 3) + ink('muted', '▏'), '', ink('muted', 'PDF only · nothing uploaded before confirmation'));
      break;
    case 'printer': case 'printers': {
      lines.push(ink('warm', state.screen === 'printer' ? '04  Where would you like to collect it?' : 'Printer locations'), '', ink('muted', `${state.showRestricted ? 'All locations · restricted choices labelled' : 'Unrestricted student printers'} · Ctrl+R toggles all`), ink('accent', 'Search  ') + short(state.input, width() - 9) + ink('muted', '▏'), '');
      menu(options(), lines, Math.max(2, Math.min(6, Math.floor((rows - 13) / 2))));
      break;
    }
    case 'mode':
      lines.push(ink('warm', '05  Choose how to print'), '', detail('Printer', state.printer.id), detail('Location', state.printer.location), '');
      menu(options(), lines, 5); break;
    case 'review': {
      const fields = rows >= 30 ? [
        detail('File', state.file.name), detail('Location', state.printer.location), detail('Printer', state.printer.id), detail('Paper', `${state.printer.paper} · ${state.printer.kind}`), detail('Printing', state.mode.label), detail('Queue', state.mode.queue), detail('Account', `${state.username}@stu`), detail('Network', state.network.label),
      ] : [detail('File', state.file.name), detail('Location', state.printer.location), detail('Printing', state.mode.label), detail('Queue', state.mode.queue)];
      lines.push(ink('warm', '06  Ready to print?'), '', ...box(fields), '');
      menu(options(), lines, 3, rows >= 30); break;
    }
    case 'submitting': {
      lines.push(ink('warm', 'Submitting · simulation'), '', detail('Queue', state.mode.queue), '');
      const stages = ['Connect to your Unix account', 'Upload PDF to a temporary file', 'Submit to the selected queue', 'Remove the temporary file'];
      stages.forEach((stage, i) => lines.push((i < state.progress ? ink('good', '✓ ') : i === state.progress ? ink('accent', '› ') : ink('muted', '· ')) + ink(i <= state.progress ? 'text' : 'muted', stage)));
      break;
    }
    case 'done':
      lines.push(ink('good', '✓ Demo submission complete'), '', ...box([detail('File', state.file.name), detail('Collect at', state.printer.location), detail('Queue', state.mode.queue), detail('Demo job', state.jobs.at(-1).id)]), '', ...wrap('Simulation only. No connection, upload or real print job was made.').map(s => ink('warm', s)), '', ink('muted', '/jobs views the simulated queue · Enter returns home')); break;
    case 'jobs':
      lines.push(ink('warm', 'Your demo jobs'), '', ink('muted', 'These are simulated jobs, not a live printer queue.'), '');
      if (!state.jobs.length) lines.push('No demo jobs yet.');
      for (const job of state.jobs.slice(-5)) lines.push(ink(job.cancelled ? 'muted' : 'accent', `${job.id}  ${job.file.name}`), ink('muted', `  ${job.mode.queue} · ${job.cancelled ? 'cancelled' : 'waiting (simulated)'}`));
      lines.push('', ...wrap('For a live queue, “no entries” means nothing is waiting. It does not confirm that a job printed.').map(s => ink('muted', s)), '', ink('muted', 'Enter returns home · /cancel selects a demo job')); break;
    case 'cancel':
      lines.push(ink('warm', 'Cancel a simulated job'), '', ink('muted', 'Enter cancels the selected demo job.'), ''); menu(options(), lines, 5); break;
    case 'help':
      lines.push(ink('warm', 'Keep it simple.'), '');
      for (const [cmd, description] of [['Print', 'Choose a file and walk through printing'], ['Printers', 'Search printer locations and restrictions'], ['Jobs', 'View simulated jobs'], ['Account', 'Set username and see account setup links'], ['Network', 'SoC, Non-SoC NUS or Non-NUS']]) lines.push(ink('accent', cmd.padEnd(12)) + description);
      lines.push('', ink('muted', 'Arrow keys choose · Enter selects · Esc goes home'), ink('muted', 'PDF paths with spaces and terminal drag-and-drop work.')); break;
  }
  if (state.error) lines.push('', ...wrap(state.error).map(s => ink('bad', s)));
  const footer = 'demo only';
  const hints = state.screen === 'home' ? 'Click an action   ↑ ↓ choose   Enter open   Ctrl+C exit' : 'Click or ↑ ↓ choose   Enter continue   Esc home';
  const budget = rows - 5;
  clickTargets = clickTargets.filter(target => target.y <= budget);
  const output = lines.slice(0, budget).map(line => '  ' + line);
  while (output.length < budget) output.push('');
  output.push('  ' + ink('muted', '─'.repeat(width())), '  ' + ink('muted', short(footer)), '  ' + ink('muted', short(hints)));
  process.stdout.write('\x1b[H\x1b[2J' + output.join('\n'));
}
function chooseFile(input) {
  if (!input.trim()) { state.file = { name: 'CS1231S.pdf', path: null, sample: true }; go('printer'); return; }
  let value = input.trim();
  if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) value = value.slice(1, -1);
  else value = value.replace(/\\([ ()'"&;\[\]])/g, '$1');
  if (value.startsWith('~/')) value = path.join(os.homedir(), value.slice(2));
  const resolved = path.resolve(value);
  try {
    const info = statSync(resolved);
    if (!info.isFile() || !/\.pdf$/i.test(resolved)) throw new Error('Choose a PDF file.');
    if (!info.size) throw new Error('This PDF is empty.');
    if (info.size > 1_000_000_000) throw new Error('SoC limits a print job to 1 GB. Choose a smaller PDF.');
    state.file = { name: path.basename(resolved), path: resolved, size: info.size, sample: false }; go('printer');
  } catch (error) { state.error = error.code === 'ENOENT' ? 'File not found. Check the path or drag the PDF here.' : error.message; render(); }
}
function startPrint(input = '') {
  state.flow = true; state.file = null; state.printer = null; state.mode = null; state.notice = '';
  state.pendingFile = input;
  if (!state.username) go('account'); else go('file', { input });
}
function runCommand(input) {
  const value = input.trim();
  if (value === '/quit' || value === '/exit') return finish();
  if (value === '/print') return startPrint();
  if (value.startsWith('/print ')) return startPrint(value.slice(7));
  if (value === '/account' || value === '/network') { state.flow = false; return go(value.slice(1)); }
  if (['/help', '/jobs', '/printers', '/cancel'].includes(value)) return go(value.slice(1));
  if (value && !value.startsWith('/')) return startPrint(value);
  if (value) { state.error = `Unknown command: ${value}. Try /help.`; render(); }
}
function enter() {
  const item = options()[state.selected];
  switch (state.screen) {
    case 'home': return runCommand(state.input || item.value);
    case 'account':
      if (!validateUsername(state.input.trim())) { state.error = 'Enter a SoC Unix username using letters, numbers, underscores or hyphens.'; return render(); }
      state.username = state.input.trim();
      return state.flow ? go('network') : go('home', { notice: 'Demo account set. No login or password was sent.' });
    case 'network':
      state.network = item.value;
      return state.flow ? go('file', { input: state.pendingFile || '' }) : go('home');
    case 'file': return chooseFile(state.input);
    case 'printer': case 'printers':
      if (!item) return;
      if (item.disabled) { state.error = 'This location has access restrictions. The demo cannot verify your eligibility.'; return render(); }
      if (state.screen === 'printers') { state.notice = `${item.value.id} · ${item.value.location}`; return go('home'); }
      state.printer = item.value; return go('mode');
    case 'mode': state.mode = item.value; return go('review');
    case 'review':
      if (item.value === 'cancel') { state.flow = false; return go('home', { notice: 'Print cancelled. Nothing was submitted.' }); }
      if (item.value === 'printer') return go('printer');
      go('submitting', { progress: 0 });
      state.timer = setInterval(() => {
        state.progress++;
        if (state.progress >= 4) {
          clearInterval(state.timer); state.timer = null;
          state.jobs.push({ id: `DEMO-${String(state.jobs.length + 1).padStart(3, '0')}`, file: { ...state.file }, printer: state.printer, mode: state.mode, cancelled: false });
          go('done');
        } else render();
      }, 350);
      return;
    case 'cancel':
      if (!item) return go('home', { notice: 'No waiting demo jobs to cancel.' });
      item.value.cancelled = true; return go('home', { notice: `${item.value.id} cancelled in the simulation.` });
    case 'done': case 'jobs': case 'help': return go('home');
  }
}
function finish() {
  if (state.stopped) return;
  state.stopped = true; clearInterval(state.timer);
  process.stdin.setRawMode(false); process.stdin.pause();
  clearTimeout(mouseFlushTimer);
  keyboard.end();
  process.stdout.write('\x1b[?1000l\x1b[?1006l\x1b[?25h\x1b[?1049l');
  console.log('Print @ SoC demo closed. No real print jobs were submitted.');
}
const keyboard = new PassThrough();
readline.emitKeypressEvents(keyboard);
let mouseInput = '', mouseFlushTimer;
process.stdin.setEncoding('utf8');
process.stdin.on('data', chunk => {
  if (state.stopped) return;
  clearTimeout(mouseFlushTimer);
  mouseInput += chunk;
  while (mouseInput) {
    const start = mouseInput.indexOf('\x1b[<');
    if (start < 0) {
      const suffix = mouseInput.endsWith('\x1b[') ? 2 : mouseInput.endsWith('\x1b') ? 1 : 0;
      keyboard.write(mouseInput.slice(0, mouseInput.length - suffix));
      mouseInput = suffix ? mouseInput.slice(-suffix) : '';
      break;
    }
    if (start > 0) { keyboard.write(mouseInput.slice(0, start)); mouseInput = mouseInput.slice(start); }
    if (state.stopped) return;
    const match = /^\x1b\[<(\d+);(\d+);(\d+)([Mm])/.exec(mouseInput);
    if (!match) {
      if (/^\x1b\[<[\d;]*$/.test(mouseInput) && mouseInput.length < 64) break;
      keyboard.write(mouseInput); mouseInput = ''; break;
    }
    mouseInput = mouseInput.slice(match[0].length);
    if (match[1] === '0' && match[4] === 'M' && state.screen !== 'submitting') {
      const x = Number(match[2]), y = Number(match[3]);
      clickTargets.find(target => target.y === y && x >= target.x && x <= target.end)?.action();
    }
  }
  if (mouseInput) mouseFlushTimer = setTimeout(() => { if (!state.stopped) keyboard.write(mouseInput); mouseInput = ''; }, 50);
});
process.stdin.setRawMode(true);
process.stdout.write('\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h');
keyboard.on('keypress', (text, key = {}) => {
  if (state.stopped) return;
  if ((key.ctrl && key.name === 'c') || (key.ctrl && key.name === 'd')) return finish();
  if (state.screen === 'submitting') return;
  if (key.name === 'escape') { state.flow = false; return go('home', { notice: 'Returned home. No new job submitted.' }); }
  if (key.name === 'return') return enter();
  if (key.ctrl && key.name === 'r' && ['printer', 'printers'].includes(state.screen)) { state.showRestricted = !state.showRestricted; state.selected = 0; state.error = ''; return render(); }
  const items = options();
  if (items.length && ['up', 'down'].includes(key.name)) { state.selected = (state.selected + (key.name === 'down' ? 1 : -1) + items.length) % items.length; state.error = ''; return render(); }
  if (state.screen === 'done' || state.screen === 'jobs' || state.screen === 'help') {
    if (text === '/') return go('home', { input: '/' });
    return;
  }
  if (!['home', 'account', 'file', 'printer', 'printers'].includes(state.screen)) return;
  if (key.name === 'backspace') state.input = [...state.input].slice(0, -1).join('');
  else if (key.ctrl && key.name === 'u') state.input = '';
  else if (key.name === 'tab' && state.screen === 'home') state.input = commands.find(c => c.startsWith(state.input)) || state.input;
  else if (text && !key.ctrl && !key.meta && !/^(f\d+|up|down|left|right|home|end|delete|pageup|pagedown|tab)$/.test(key.name || '')) state.input += clean(text);
  state.input = state.input.slice(0, 4096); state.error = ''; state.selected = 0; render();
});
process.stdout.on('resize', render);
process.on('SIGTERM', finish);
process.on('uncaughtException', error => { finish(); console.error(error.message); process.exitCode = 1; });
render();
