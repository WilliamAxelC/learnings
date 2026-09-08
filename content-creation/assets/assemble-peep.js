#!/usr/bin/env node

/**
 * Open Peeps Character Assembler
 * 
 * Zero-dependency SVG compositor for Open Peeps modular parts.
 * Assembles hair, face, facial-hair, accessory, and body into a single crisp SVG.
 * 
 * Usage CLI:
 *   node assemble-peep.js --hair bun-curly --face smile --body button-shirt --out my-peep.svg
 *   node assemble-peep.js --hair afro --face driven --body hoodie --facial-hair goatee --out founder.svg
 *   node assemble-peep.js --list
 * 
 * Usage Programmatic:
 *   const { assemblePeep } = require('./assemble-peep');
 *   const svgString = assemblePeep({ hair: 'bun-curly', face: 'smile', body: 'button-shirt' });
 */

const fs = require('fs');
const path = require('path');

const ASSETS_DIR = __dirname;
const CHARS_DIR = path.join(ASSETS_DIR, 'characters');

function normalizeName(str) {
  if (!str) return '';
  return str
    .trim()
    .replace(/([a-z])([A-Z])/g, '$1-$2')
    .replace(/[\s_]+/g, '-')
    .toLowerCase();
}

function getInnerSvg(filePath) {
  if (!filePath || !fs.existsSync(filePath)) return null;
  const content = fs.readFileSync(filePath, 'utf-8');
  return content
    .replace(/^<\?xml[^>]*\?>/i, '')
    .replace(/<svg[^>]*>/i, '')
    .replace(/<\/svg>/i, '')
    .trim();
}

function resolvePart(subdir, name) {
  if (!name || name === 'none') return null;
  const norm = normalizeName(name);
  const directPath = path.join(CHARS_DIR, subdir, `${norm}.svg`);
  if (fs.existsSync(directPath)) return directPath;

  // Search if in bodies (bust, standing, sitting)
  if (subdir.startsWith('bodies')) {
    for (const sub of ['bust', 'standing', 'sitting']) {
      const p = path.join(CHARS_DIR, 'bodies', sub, `${norm}.svg`);
      if (fs.existsSync(p)) return p;
    }
  }

  // Case-insensitive fallback
  const folder = path.join(CHARS_DIR, subdir);
  if (fs.existsSync(folder)) {
    const files = fs.readdirSync(folder);
    for (const f of files) {
      if (normalizeName(f.replace('.svg', '')) === norm) {
        return path.join(folder, f);
      }
    }
  }
  return null;
}

function assemblePeep(options = {}) {
  const {
    hair = null,
    face = null,
    facialHair = null,
    accessory = null,
    body = null,
    strokeColor = null,
    backgroundColor = null
  } = options;

  let bodyFile = null;
  let bodyType = 'bust';

  if (body) {
    const normBody = normalizeName(body);
    for (const type of ['bust', 'standing', 'sitting']) {
      const p = path.join(CHARS_DIR, 'bodies', type, `${normBody}.svg`);
      if (fs.existsSync(p)) {
        bodyFile = p;
        bodyType = type;
        break;
      }
    }
    if (!bodyFile) {
      bodyFile = resolvePart('bodies/bust', body);
    }
  }

  const hairFile = hair ? resolvePart('hair', hair) : null;
  const faceFile = face ? resolvePart('faces', face) : null;
  const facialHairFile = facialHair ? resolvePart('facial-hair', facialHair) : null;
  const accessoryFile = accessory ? resolvePart('accessories', accessory) : null;

  const hairSvg = getInnerSvg(hairFile);
  const faceSvg = getInnerSvg(faceFile);
  const facialHairSvg = getInnerSvg(facialHairFile);
  const accessorySvg = getInnerSvg(accessoryFile);
  const bodySvg = getInnerSvg(bodyFile);

  let viewBox = '0 0 1050 1200';
  let headTransform = 'translate(225 0)';

  if (bodyType === 'standing') {
    viewBox = '-300 0 1600 2600';
    headTransform = 'translate(225 0)';
  } else if (bodyType === 'sitting') {
    viewBox = '-200 0 1400 1600';
    headTransform = 'translate(225 0)';
  }

  if (options.viewBox) {
    viewBox = options.viewBox;
  }

  let headGroup = '';
  if (hairSvg || faceSvg || facialHairSvg || accessorySvg) {
    headGroup = `
  <g id="head" transform="${headTransform}">
    ${hairSvg ? `<g id="hair">${hairSvg}</g>` : ''}
    ${faceSvg ? `<g id="face" transform="translate(159 186)">${faceSvg}</g>` : ''}
    ${facialHairSvg ? `<g id="facial-hair" transform="translate(123 338)">${facialHairSvg}</g>` : ''}
    ${accessorySvg ? `<g id="accessory" transform="translate(47 241)">${accessorySvg}</g>` : ''}
  </g>`;
  }

  const bodyGroup = bodySvg ? `
  <g id="body">
    ${bodySvg}
  </g>` : '';

  let customStyles = '';
  if (strokeColor) {
    customStyles += ` path, line, circle, rect { stroke: ${strokeColor} !important; }`;
  }
  if (backgroundColor) {
    customStyles += ` path[fill]:not([fill="none"]):not([fill="#000000"]):not([fill="#000"]) { fill: ${backgroundColor} !important; }`;
  }
  const styleBlock = customStyles ? `\n  <style>${customStyles}</style>` : '';

  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${viewBox}" width="100%" height="100%" fill="none">${styleBlock}${bodyGroup}${headGroup}
</svg>`;
}

function listAvailable() {
  console.log("=== Open Peeps Modular Components ===");
  const categories = [
    { name: 'Faces (Expressions)', dir: 'faces' },
    { name: 'Hair Styles', dir: 'hair' },
    { name: 'Facial Hair', dir: 'facial-hair' },
    { name: 'Accessories', dir: 'accessories' },
    { name: 'Bust Bodies', dir: 'bodies/bust' },
    { name: 'Standing Bodies', dir: 'bodies/standing' },
    { name: 'Sitting Bodies', dir: 'bodies/sitting' }
  ];

  for (const cat of categories) {
    const dirPath = path.join(CHARS_DIR, cat.dir);
    if (fs.existsSync(dirPath)) {
      const items = fs.readdirSync(dirPath)
        .filter(f => f.endsWith('.svg'))
        .map(f => f.replace('.svg', ''));
      console.log(`\n-- ${cat.name} (${items.length}) --`);
      console.log(items.join(', '));
    }
  }
}

// CLI runner
if (require.main === module) {
  const args = process.argv.slice(2);
  if (args.includes('--list') || args.includes('-l')) {
    listAvailable();
    process.exit(0);
  }

  const getArg = (flag) => {
    const idx = args.indexOf(flag);
    return idx !== -1 && args[idx + 1] ? args[idx + 1] : null;
  };

  const hair = getArg('--hair') || getArg('-h');
  const face = getArg('--face') || getArg('-f');
  const body = getArg('--body') || getArg('-b');
  const facialHair = getArg('--facial-hair') || getArg('--beard');
  const accessory = getArg('--accessory') || getArg('-a');
  const out = getArg('--out') || getArg('-o');

  if (!hair && !face && !body) {
    console.log(`Open Peeps Character Assembler
Usage:
  node assemble-peep.js --hair <name> --face <name> --body <name> [--facial-hair <name>] [--accessory <name>] [--out <output.svg>]
  node assemble-peep.js --list

Examples:
  node assemble-peep.js --hair bun-curly --face smile --body button-shirt --out barista.svg
  node assemble-peep.js --hair afro --face driven --body hoodie --out founder.svg
`);
    process.exit(0);
  }

  const svg = assemblePeep({ hair, face, body, facialHair, accessory });

  if (out) {
    const outPath = path.resolve(process.cwd(), out);
    fs.mkdirSync(path.dirname(outPath), { recursive: true });
    fs.writeFileSync(outPath, svg, 'utf-8');
    console.log(`Assembled Open Peep character saved to: ${outPath}`);
  } else {
    console.log(svg);
  }
}

module.exports = {
  assemblePeep,
  normalizeName
};
