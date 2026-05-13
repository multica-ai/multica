import { useEffect, useMemo, useState } from "react";
import countriesData from "@countrystatecity/countries/data/countries.json";
import countryDirsData from "./generated/country-dirs.json";
import stateDirsData from "./generated/state-dirs.json";

export type LocaleCode = "en" | "zh-Hans";

export interface LocalizedName {
  en: string;
  zh: string;
}

export interface CityOption {
  code: string;
  name: LocalizedName;
}

export interface RegionOption {
  code: string;
  name: LocalizedName;
  cities: CityOption[];
}

export interface CountryOption {
  code: string;
  name: LocalizedName;
  regions: RegionOption[];
}

type TranslationMap = Record<string, string>;

type CountryData = {
  iso2: string;
  name: string;
  native?: string | null;
  translations?: TranslationMap;
};

type StateData = {
  iso2: string;
  name: string;
  native?: string | null;
  translations?: TranslationMap;
};

type CityData = {
  id?: number;
  name: string;
  native?: string | null;
  translations?: TranslationMap;
};

type CountryDirEntry = {
  code: string;
  dir: string;
};

type StateDirMap = Record<string, Record<string, string>>;

const countryNameFormatterCache = new Map<LocaleCode, Intl.DisplayNames>();
const countryMetaCache = new Map<string, Promise<CountryData | null>>();
const statesCache = new Map<string, Promise<StateData[]>>();
const citiesCache = new Map<string, Promise<CityData[]>>();
const countryDirMap = new Map((countryDirsData as CountryDirEntry[]).map((entry) => [entry.code, entry.dir]));
const stateDirMap = stateDirsData as StateDirMap;
const englishCollator = new Intl.Collator("en", { sensitivity: "base", numeric: true });
const pinyinCollator = new Intl.Collator("zh-Hans-CN-u-co-pinyin", { sensitivity: "base", numeric: true });

const getCountryNameFormatter = (locale: LocaleCode) => {
  const cached = countryNameFormatterCache.get(locale);
  if (cached) return cached;
  const formatter = new Intl.DisplayNames([locale === "zh-Hans" ? "zh-Hans" : "en"], { type: "region" });
  countryNameFormatterCache.set(locale, formatter);
  return formatter;
};

const localizedCountryName = (country: CountryData): LocalizedName => {
  const metaZh = country.translations?.["zh-CN"] ?? country.translations?.zh;
  const zh = metaZh ?? getCountryNameFormatter("zh-Hans").of(country.iso2) ?? country.native ?? country.name;
  const en = getCountryNameFormatter("en").of(country.iso2) ?? country.name;
  return { en, zh };
};

const CN_REGION_ZH: Record<string, string> = {
  AH: "安徽",
  BJ: "北京",
  CQ: "重庆",
  FJ: "福建",
  GS: "甘肃",
  GD: "广东",
  GX: "广西",
  GZ: "贵州",
  HI: "海南",
  HE: "河北",
  HL: "黑龙江",
  HA: "河南",
  HK: "香港",
  HB: "湖北",
  HN: "湖南",
  JS: "江苏",
  JX: "江西",
  JL: "吉林",
  LN: "辽宁",
  MO: "澳门",
  NM: "内蒙古",
  NX: "宁夏",
  QH: "青海",
  SN: "陕西",
  SD: "山东",
  SH: "上海",
  SX: "山西",
  SC: "四川",
  TJ: "天津",
  XJ: "新疆",
  XZ: "西藏",
  YN: "云南",
  ZJ: "浙江",
};

const localizedSubdivisionName = (name: string, native?: string | null, translations?: TranslationMap, zhOverride?: string): LocalizedName => {
  const zh = zhOverride ?? translations?.["zh-CN"] ?? translations?.zh ?? native ?? name;
  return { en: name, zh };
};

async function loadPackageJson<T>(path: string): Promise<T> {
  const module = await import(/* webpackInclude: /\.json$/ */ /* webpackMode: "lazy" */ `@countrystatecity/countries/data/${path}.json`);
  return module.default as T;
}

const countryData = countriesData as CountryData[];

export const COUNTRY_OPTIONS: CountryOption[] = localizedSort(countryData.map((country) => ({
  code: country.iso2,
  name: localizedCountryName(country),
  regions: [],
})), "en");

export const normalizeLocale = (language?: string): LocaleCode => language?.startsWith("zh") ? "zh-Hans" : "en";
export const localizedName = (name: LocalizedName, locale: LocaleCode) => locale === "zh-Hans" ? name.zh : name.en;
export const countryByCode = (code?: string | null) => COUNTRY_OPTIONS.find((country) => country.code === code);
export const regionByCode = (country: CountryOption | undefined, code?: string | null) => country?.regions.find((region) => region.code === code);
export const cityByCode = (region: RegionOption | undefined, code?: string | null) => region?.cities.find((city) => city.code === code);

const normalizeLookupValue = (value?: string | null) => value?.trim().toLocaleLowerCase() ?? "";
const optionMatches = (option: { code: string; name: LocalizedName }, value?: string | null) => {
  const normalized = normalizeLookupValue(value);
  if (!normalized) return false;
  return [option.code, option.name.en, option.name.zh]
    .map((candidate) => normalizeLookupValue(candidate))
    .includes(normalized);
};

export function localizedSort<T extends { name: LocalizedName }>(options: T[], locale: LocaleCode): T[] {
  const collator = locale === "zh-Hans" ? pinyinCollator : englishCollator;
  const field = locale === "zh-Hans" ? "zh" : "en";
  return [...options].sort((a, b) => {
    const primary = collator.compare(a.name[field], b.name[field]);
    return primary || englishCollator.compare(a.name.en, b.name.en);
  });
}

export function countryDisplayName(codeOrName: string | null | undefined, locale: LocaleCode) {
  if (!codeOrName) return "";
  const country = countryByCode(codeOrName);
  return country ? localizedName(country.name, locale) : codeOrName;
}

export async function loadCountryOption(countryCode: string): Promise<CountryOption | undefined> {
  if (!countryCode) return undefined;
  const country = countryByCode(countryCode);
  if (!country) return undefined;

  let metaPromise = countryMetaCache.get(countryCode);
  if (!metaPromise) {
    const countryDir = countryDirMap.get(countryCode);
    metaPromise = countryDir ? loadPackageJson<CountryData>(`${countryDir}/meta`) : Promise.resolve(null);
    countryMetaCache.set(countryCode, metaPromise);
  }

  const meta = await metaPromise;
  return meta ? { ...country, name: localizedCountryName(meta) } : country;
}

export async function loadRegionOptions(countryCode: string, locale: LocaleCode = "en"): Promise<RegionOption[]> {
  if (!countryCode) return [];
  let promise = statesCache.get(countryCode);
  if (!promise) {
    const countryDir = countryDirMap.get(countryCode);
    promise = countryDir ? loadPackageJson<StateData[]>(`${countryDir}/states`) : Promise.resolve([]);
    statesCache.set(countryCode, promise);
  }

  const states = await promise;
  return localizedSort(states.map((state) => ({
    code: state.iso2,
    name: localizedSubdivisionName(state.name, state.native, state.translations, countryCode === "CN" ? CN_REGION_ZH[state.iso2] : undefined),
    cities: [],
  })), locale);
}

export async function loadCityOptions(countryCode: string, regionCode: string, locale: LocaleCode = "en"): Promise<CityOption[]> {
  if (!countryCode || !regionCode) return [];
  const cacheKey = `${countryCode}:${regionCode}`;
  let promise = citiesCache.get(cacheKey);
  if (!promise) {
    const countryDir = countryDirMap.get(countryCode);
    const stateDir = stateDirMap[countryCode]?.[regionCode];
    promise = countryDir && stateDir ? loadPackageJson<CityData[]>(`${countryDir}/${stateDir}/cities`) : Promise.resolve([]);
    citiesCache.set(cacheKey, promise);
  }

  const cities = await promise;
  return localizedSort(cities.map((city) => ({
    code: city.id != null ? String(city.id) : city.name,
    name: localizedSubdivisionName(city.name, city.native, city.translations),
  })), locale);
}

export async function findRegionCode(countryCode: string, region?: string | null): Promise<string> {
  if (!countryCode || !region) return "";
  const regions = await loadRegionOptions(countryCode);
  return regions.find((option) => optionMatches(option, region))?.code ?? "";
}

export async function findCityCode(countryCode: string, regionCode: string, city?: string | null): Promise<string> {
  if (!countryCode || !regionCode || !city) return "";
  const cities = await loadCityOptions(countryCode, regionCode);
  return cities.find((option) => optionMatches(option, city))?.code ?? "";
}

export function useRegionOptions(countryCode: string, locale: LocaleCode = "en") {
  const [regions, setRegions] = useState<RegionOption[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setRegions([]);
    if (!countryCode) return;
    setIsLoading(true);
    loadRegionOptions(countryCode, locale)
      .then((next) => {
        if (!cancelled) setRegions(next);
      })
      .catch(() => {
        if (!cancelled) setRegions([]);
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [countryCode, locale, nonce]);

  return { regions, isLoading, retry: () => setNonce((current) => current + 1) };
}

export function useCityOptions(countryCode: string, regionCode: string, locale: LocaleCode = "en") {
  const [cities, setCities] = useState<CityOption[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setCities([]);
    if (!countryCode || !regionCode) return;
    setIsLoading(true);
    loadCityOptions(countryCode, regionCode, locale)
      .then((next) => {
        if (!cancelled) setCities(next);
      })
      .catch(() => {
        if (!cancelled) setCities([]);
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [countryCode, regionCode, locale, nonce]);

  return { cities, isLoading, retry: () => setNonce((current) => current + 1) };
}

export function useLocationSelection(countryCode: string, regionCode: string, locale: LocaleCode = "en") {
  const { regions, isLoading: regionsLoading, retry: retryRegions } = useRegionOptions(countryCode, locale);
  const selectedRegion = useMemo(() => regions.find((region) => region.code === regionCode), [regionCode, regions]);
  const { cities, isLoading: citiesLoading, retry: retryCities } = useCityOptions(countryCode, regionCode, locale);

  return { regions, cities, selectedRegion, regionsLoading, citiesLoading, retryRegions, retryCities };
}
