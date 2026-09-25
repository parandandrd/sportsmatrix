// A weather location is a latitude and longitude, as Google Maps copies them:
// "41.8858, -87.6181". The Pi checks it properly; this only says whether what
// was typed is worth sending.

// ParseLocation tidies typed coordinates into "lat, lon", or returns null for
// anything that isn't two numbers in range.
export function ParseLocation(text) {
    const parts = String(text || '').split(/[\s,]+/).filter((p) => p !== '');
    if (parts.length !== 2 || !parts.every((p) => /^[-+]?\d+(\.\d+)?$/.test(p))) {
        return null;
    }
    const [lat, lon] = parts.map(Number);
    if (Math.abs(lat) > 90 || Math.abs(lon) > 180) {
        return null;
    }
    return `${lat}, ${lon}`;
}
