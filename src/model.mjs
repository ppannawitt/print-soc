import { readFileSync } from 'node:fs';

export const printers = JSON.parse(readFileSync(new URL('../data/printers.json', import.meta.url), 'utf8'));

export const links = {
  account: 'https://mysoc.nus.edu.sg/~newacct/',
  services: 'https://mysoc.nus.edu.sg/~myacct/services.cgi',
  keys: 'https://dochub.comp.nus.edu.sg/cf/services/network/skeys',
  jump: 'https://dochub.comp.nus.edu.sg/cf/guides/sjump/start',
};

export const networks = [
  { id: 'soc', label: 'SoC', route: 'Direct SSH' },
  { id: 'nus', label: 'Non-SoC NUS', route: 'SSH jump host' },
  { id: 'outside', label: 'Non-NUS', route: 'NUS VPN → SSH jump host' },
];

export function queueMode(printer, queue) {
  if (!printer.queues.includes(queue)) throw new Error('Queue is not in this printer catalog.');
  const noBanner = queue.includes('-nb');
  const sides = queue.endsWith('-sx') ? 'single' : queue.endsWith('-dx') || noBanner ? 'double' : printer.kind === 'colour' ? 'single' : 'double';
  const banner = printer.kind === 'colour' || printer.banner === 'No' || noBanner ? false : printer.banner == null ? null : true;
  return { queue, sides, banner, label: `${sides === 'single' ? 'Single' : 'Double'}-sided${banner == null ? ' · banner unspecified' : banner ? ' · banner page' : ' · no banner'}` };
}

export function printerModes(printer) {
  return printer.queues.map(queue => queueMode(printer, queue));
}

export function searchPrinters(query = '', showRestricted = false) {
  const words = query.toLowerCase().trim().split(/\s+/).filter(Boolean);
  return printers.filter(p => (showRestricted || p.access === 'public') && words.every(word => `${p.id} ${p.location} ${p.kind} ${p.paper} ${p.access}`.toLowerCase().includes(word)));
}

export function validateUsername(value) {
  return /^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$/.test(value);
}
