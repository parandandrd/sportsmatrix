// A weather location is a latitude and longitude, the ways maps show them:
// "41.8858, -87.6181" as Google Maps copies them, 41°52'58.0"N 87°55'27.0"W
// as it shows a dropped pin, or "41.8828° N, 87.9242° W" as Apple Maps copies
// them. The Pi checks it properly; this reads it the same way
// (ParseLocation in internal/weather) to say whether it is worth sending, and
// sends it as decimal degrees.

// degrees is a number of degrees: decimal, or whole with minutes and perhaps
// seconds.
const degrees = String.raw`([-+]?\d+(?:\.\d+)?)\s*°?\s*(?:(\d+(?:\.\d+)?)\s*'\s*)?(?:(\d+(?:\.\d+)?)\s*"\s*)?`;

// coordinate is one of the two numbers, signed or with a compass letter before
// or after it: one or the other, so that in "N41 W87" the W can't be taken for
// the first number's.
const coordinate = String.raw`(?:([NSEW])\s*` + degrees + '|' + degrees + '([NSEW])?)';

const locationPattern = new RegExp(String.raw`^\s*` + coordinate + String.raw`(?:\s*[,;]\s*|\s+)` + coordinate + String.raw`\s*$`, 'i');

// the marks phones and maps use for degrees, minutes and seconds, as the ones
// locationPattern looks for
const marks = [[/[º˚]/g, '°'], [/[′’‘]/g, "'"], [/[″“”]/g, '"'], [/''/g, '"']];

// parseCoordinate turns one coordinate's parts, as coordinate matches them,
// into [decimal degrees, compass letter], or null.
function parseCoordinate(parts) {
    let [letter, deg, mins, secs] = parts;
    if (deg === undefined) {
        [deg, mins, secs, letter] = parts.slice(4);
    }
    const dir = letter ? letter.toUpperCase() : '';
    const signed = deg[0] === '-' || deg[0] === '+';

    if ((dir && signed) || (mins !== undefined && deg.includes('.'))
        || (secs !== undefined && (mins === undefined || mins.includes('.')))) {
        return null;
    }

    let v = Number(deg.replace(/^[-+]/, ''));
    for (const [part, per] of [[mins, 60], [secs, 3600]]) {
        if (part === undefined) {
            continue;
        }
        const n = Number(part);
        if (n >= 60) {
            return null;
        }
        v += n / per;
    }

    if (deg[0] === '-' || dir === 'S' || dir === 'W') {
        v = -v;
    }
    return [v, dir];
}

const isLatitude = (dir) => dir === 'N' || dir === 'S';
const isLongitude = (dir) => dir === 'E' || dir === 'W';

// ParseLocation tidies typed coordinates into "lat, lon" in decimal degrees,
// as the Pi saves them, or returns null for anything it can't read as a
// latitude and longitude in range.
export function ParseLocation(text) {
    let s = String(text || '');
    for (const [from, to] of marks) {
        s = s.replace(from, to);
    }
    const m = locationPattern.exec(s);
    if (!m) {
        return null;
    }

    let a = parseCoordinate(m.slice(1, 9));
    let b = parseCoordinate(m.slice(9, 17));
    if (!a || !b) {
        return null;
    }
    if (isLongitude(a[1]) || isLatitude(b[1])) {
        [a, b] = [b, a];
    }
    if (isLongitude(a[1]) || isLatitude(b[1])) {
        return null;
    }

    const [lat, lon] = [a[0], b[0]];
    if (Math.abs(lat) > 90 || Math.abs(lon) > 180) {
        return null;
    }
    return `${lat.toFixed(4)}, ${lon.toFixed(4)}`;
}
