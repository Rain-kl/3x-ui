import { describe, expect, it } from 'vitest';

import {
  parseOutboundTagsFromXrayConfigObj,
  parseRoutingRulesFromXrayConfigObj,
} from '@/pages/xray/routing/helpers';

const remoteRules = [
  { type: 'field', outboundTag: 'remote-proxy', domain: ['geosite:google'] },
  { type: 'field', outboundTag: 'direct', ip: ['geoip:private'] },
];

describe('parseRoutingRulesFromXrayConfigObj', () => {
  it('extracts rules from the JSON-string envelope the xray/ API returns', () => {
    const obj = JSON.stringify({
      xraySetting: { routing: { rules: remoteRules } },
      inboundTags: [],
    });
    expect(parseRoutingRulesFromXrayConfigObj(obj)).toEqual(remoteRules);
  });

  it('extracts rules when xraySetting is itself a JSON string', () => {
    const obj = JSON.stringify({
      xraySetting: JSON.stringify({ routing: { rules: remoteRules } }),
    });
    expect(parseRoutingRulesFromXrayConfigObj(obj)).toEqual(remoteRules);
  });

  it('extracts rules from an already-parsed payload object', () => {
    expect(
      parseRoutingRulesFromXrayConfigObj({
        xraySetting: { routing: { rules: remoteRules } },
      }),
    ).toEqual(remoteRules);
  });

  it('returns an empty list when the node has no rules', () => {
    expect(
      parseRoutingRulesFromXrayConfigObj(
        JSON.stringify({ xraySetting: { routing: { rules: null } } }),
      ),
    ).toEqual([]);
    expect(
      parseRoutingRulesFromXrayConfigObj(JSON.stringify({ xraySetting: { routing: {} } })),
    ).toEqual([]);
  });

  it('returns null for a malformed envelope so callers refuse to save', () => {
    expect(parseRoutingRulesFromXrayConfigObj('{')).toBeNull();
    expect(parseRoutingRulesFromXrayConfigObj({})).toBeNull();
    expect(parseRoutingRulesFromXrayConfigObj(null)).toBeNull();
    expect(parseRoutingRulesFromXrayConfigObj({ inboundTags: [] })).toBeNull();
  });
});

describe('parseOutboundTagsFromXrayConfigObj', () => {
  it('extracts outbound tags from the JSON-string xray/ envelope', () => {
    const obj = JSON.stringify({
      xraySetting: { routing: { rules: remoteRules } },
      outboundTags: ['direct', 'blocked', 'worker-proxy'],
    });
    expect(parseOutboundTagsFromXrayConfigObj(obj)).toEqual(['direct', 'blocked', 'worker-proxy']);
  });

  it('parses outboundTags when the field itself is a JSON string', () => {
    expect(
      parseOutboundTagsFromXrayConfigObj({
        outboundTags: '["direct","blocked"]',
      }),
    ).toEqual(['direct', 'blocked']);
  });

  it('returns an empty list when outboundTags is missing or malformed', () => {
    expect(parseOutboundTagsFromXrayConfigObj(JSON.stringify({ xraySetting: {} }))).toEqual([]);
    expect(parseOutboundTagsFromXrayConfigObj({ outboundTags: null })).toEqual([]);
    expect(parseOutboundTagsFromXrayConfigObj('{')).toEqual([]);
  });
});
