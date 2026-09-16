// The screen schedule is kept as cron expressions. Most say "at this time
// every day", and those are edited as a time. Anything else -- weekdays only,
// every few hours -- is kept as it was written.

const daily = /^(\d{1,2}) (\d{1,2}) \* \* \*$/;

// ParseCron reads a cron expression as { time: "HH:MM" } when it runs once a
// day at a set time, or { spec } when it doesn't.
export function ParseCron(spec) {
    const m = daily.exec(spec.trim());
    if (m) {
        const minute = Number(m[1]);
        const hour = Number(m[2]);
        if (minute < 60 && hour < 24) {
            return { time: `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}` };
        }
    }
    return { spec };
}

// DailyCron is the cron expression for "every day at time", time as HH:MM.
export function DailyCron(time) {
    const [hour, minute] = time.split(':').map(Number);
    return `${minute} ${hour} * * *`;
}

// ToCron turns an entry ParseCron made back into an expression.
export function ToCron(entry) {
    return entry.time !== undefined ? DailyCron(entry.time) : entry.spec;
}
