import { ParseLocation } from './location';

test('ParseLocation takes coordinates as Google Maps copies them', () => {
    expect(ParseLocation('41.8858, -87.6181')).toBe('41.8858, -87.6181');
    expect(ParseLocation('41.8858,-87.6181')).toBe('41.8858, -87.6181');
    expect(ParseLocation('  41.8858   -87.6181 ')).toBe('41.8858, -87.6181');
    expect(ParseLocation('-33.8688, 151.2093')).toBe('-33.8688, 151.2093');
});

test('ParseLocation refuses what is not a latitude and longitude', () => {
    expect(ParseLocation('')).toBeNull();
    expect(ParseLocation('Chicago, IL')).toBeNull();
    expect(ParseLocation('60601')).toBeNull();
    expect(ParseLocation('91, 10')).toBeNull();
    expect(ParseLocation('41, 181')).toBeNull();
    expect(ParseLocation('41, -87, 3')).toBeNull();
    expect(ParseLocation(null)).toBeNull();
});
