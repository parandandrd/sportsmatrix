// Display times are Go durations, as the config file and the API write them:
// "10s", "1m30s", "2m".

const unit = { h: 3600, m: 60, s: 1 };

// DelaySeconds reads a duration made of hours, minutes and seconds.
export function DelaySeconds(text) {
    const parts = String(text || '').match(/(\d+(?:\.\d+)?)(h|m|s)/g);
    if (!parts || parts.join('') !== text) {
        return NaN;
    }
    return parts.reduce((total, p) => total + parseFloat(p) * unit[p.slice(-1)], 0);
}

const choices = ['5s', '10s', '15s', '20s', '30s', '45s', '1m', '1m30s', '2m', '5m', '10m'];

// DelayChoices are the display times to offer a board: the usual ones it can
// take, and whatever it is set to now, even if that is none of them.
export function DelayChoices(current, min) {
    const floor = DelaySeconds(min) || 0;
    const out = choices.filter((c) => DelaySeconds(c) >= floor);
    if (current && !out.includes(current)) {
        out.push(current);
        out.sort((a, b) => DelaySeconds(a) - DelaySeconds(b));
    }
    return out;
}
