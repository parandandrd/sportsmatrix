import { ParseLocation } from './location';

test('ParseLocation takes coordinates as Google Maps copies them', () => {
    expect(ParseLocation('41.8858, -87.6181')).toBe('41.8858, -87.6181');
    expect(ParseLocation('41.8858,-87.6181')).toBe('41.8858, -87.6181');
    expect(ParseLocation('  41.8858   -87.6181 ')).toBe('41.8858, -87.6181');
    expect(ParseLocation('-33.8688, 151.2093')).toBe('-33.8688, 151.2093');
    expect(ParseLocation('41.5, -87')).toBe('41.5000, -87.0000');
});

test('ParseLocation takes degrees, minutes and seconds', () => {
    expect(ParseLocation(`41°52'58.0"N 87°55'27.0"W`)).toBe('41.8828, -87.9242');
    expect(ParseLocation(`33°52'07.7"S 151°12'33.5"E`)).toBe('-33.8688, 151.2093');
    expect(ParseLocation('41° 52′ 58″ N, 87° 55′ 27″ W')).toBe('41.8828, -87.9242');
    expect(ParseLocation("41°52.967'N 87°55.450'W")).toBe('41.8828, -87.9242');
});

test('ParseLocation takes compass letters', () => {
    expect(ParseLocation('41.8828° N, 87.9242° W')).toBe('41.8828, -87.9242');
    expect(ParseLocation('N41.8828 W87.9242')).toBe('41.8828, -87.9242');
    expect(ParseLocation('87.9242 W, 41.8828 N')).toBe('41.8828, -87.9242');
});

test('ParseLocation refuses what is not a latitude and longitude', () => {
    expect(ParseLocation('')).toBeNull();
    expect(ParseLocation('Chicago, IL')).toBeNull();
    expect(ParseLocation('60601')).toBeNull();
    expect(ParseLocation('41.8858')).toBeNull();
    expect(ParseLocation('91, 10')).toBeNull();
    expect(ParseLocation('41, 181')).toBeNull();
    expect(ParseLocation('41, -87, 3')).toBeNull();
    expect(ParseLocation(null)).toBeNull();
    expect(ParseLocation(`41°60'00"N 87°55'27"W`)).toBeNull();
    expect(ParseLocation(`41.5°52'N 87°55'W`)).toBeNull();
    expect(ParseLocation(`41° 58"N 87° 27"W`)).toBeNull();
    expect(ParseLocation('-41.8828 N, 87.9242 W')).toBeNull();
    expect(ParseLocation('41.8828 N, 87.9242 N')).toBeNull();
    expect(ParseLocation('41.8828 E, 87.9242 W')).toBeNull();
});
