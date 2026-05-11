import { useEffect, useMemo, useState } from "react";
import countriesData from "@countrystatecity/countries/data/countries.json";
import {
  getCitiesOfState,
  getCountryByCode,
  getStatesOfCountry,
  type ICity,
  type ICountry,
  type ICountryMeta,
  type IState,
} from "@countrystatecity/countries";

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

type CountryData = ICountry & { translations?: Record<string, string> };
type StateData = IState & { translations?: Record<string, string> };
type CityData = ICity & { translations?: Record<string, string> };

const countryNameFormatterCache = new Map<LocaleCode, Intl.DisplayNames>();
const countryMetaCache = new Map<string, Promise<ICountryMeta | null>>();
const statesCache = new Map<string, Promise<StateData[]>>();
const citiesCache = new Map<string, Promise<CityData[]>>();

const getCountryNameFormatter = (locale: LocaleCode) => {
  const cached = countryNameFormatterCache.get(locale);
  if (cached) return cached;
  const formatter = new Intl.DisplayNames([locale === "zh-Hans" ? "zh-Hans" : "en"], { type: "region" });
  countryNameFormatterCache.set(locale, formatter);
  return formatter;
};

const localizedCountryName = (country: CountryData): LocalizedName => {
  const metaZh = country.translations?.["zh-CN"] ?? country.translations?.zh;
  const zh = metaZh ?? getCountryNameFormatter("zh-Hans").of(country.iso2) ?? country.name;
  const en = getCountryNameFormatter("en").of(country.iso2) ?? country.name;
  return { en, zh };
};

const localizedSubdivisionName = (name: string, native?: string | null, translations?: Record<string, string>): LocalizedName => {
  const zh = translations?.["zh-CN"] ?? translations?.zh ?? native ?? name;
  return { en: name, zh };
};

const countryData = countriesData as CountryData[];

export const COUNTRY_OPTIONS: CountryOption[] = countryData
  .map((country) => ({
    code: country.iso2,
    name: localizedCountryName(country),
    regions: [],
  }))
  .sort((a, b) => a.name.en.localeCompare(b.name.en));

export const normalizeLocale = (language?: string): LocaleCode => language?.startsWith("zh") ? "zh-Hans" : "en";
export const localizedName = (name: LocalizedName, locale: LocaleCode) => locale === "zh-Hans" ? name.zh : name.en;
export const countryByCode = (code?: string | null) => COUNTRY_OPTIONS.find((country) => country.code === code);
export const regionByCode = (country: CountryOption | undefined, code?: string | null) => country?.regions.find((region) => region.code === code);
export const cityByCode = (region: RegionOption | undefined, code?: string | null) => region?.cities.find((city) => city.code === code);

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
    metaPromise = getCountryByCode(countryCode);
    countryMetaCache.set(countryCode, metaPromise);
  }

  const meta = await metaPromise;
  return meta ? { ...country, name: localizedCountryName(meta) } : country;
}

export async function loadRegionOptions(countryCode: string): Promise<RegionOption[]> {
  if (!countryCode) return [];
  let promise = statesCache.get(countryCode);
  if (!promise) {
    promise = getStatesOfCountry(countryCode) as Promise<StateData[]>;
    statesCache.set(countryCode, promise);
  }

  const states = await promise;
  return states.map((state) => ({
    code: state.iso2,
    name: localizedSubdivisionName(state.name, state.native, state.translations),
    cities: [],
  }));
}

export async function loadCityOptions(countryCode: string, regionCode: string): Promise<CityOption[]> {
  if (!countryCode || !regionCode) return [];
  const cacheKey = `${countryCode}:${regionCode}`;
  let promise = citiesCache.get(cacheKey);
  if (!promise) {
    promise = getCitiesOfState(countryCode, regionCode) as Promise<CityData[]>;
    citiesCache.set(cacheKey, promise);
  }

  const cities = await promise;
  return cities.map((city) => ({
    code: String(city.id),
    name: localizedSubdivisionName(city.name, city.native, city.translations),
  }));
}

export function useRegionOptions(countryCode: string) {
  const [regions, setRegions] = useState<RegionOption[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setRegions([]);
    if (!countryCode) return;
    setIsLoading(true);
    loadRegionOptions(countryCode)
      .then((next) => {
        if (!cancelled) setRegions(next);
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [countryCode]);

  return { regions, isLoading };
}

export function useCityOptions(countryCode: string, regionCode: string) {
  const [cities, setCities] = useState<CityOption[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setCities([]);
    if (!countryCode || !regionCode) return;
    setIsLoading(true);
    loadCityOptions(countryCode, regionCode)
      .then((next) => {
        if (!cancelled) setCities(next);
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [countryCode, regionCode]);

  return { cities, isLoading };
}

export function useLocationSelection(countryCode: string, regionCode: string) {
  const { regions, isLoading: regionsLoading } = useRegionOptions(countryCode);
  const selectedRegion = useMemo(() => regions.find((region) => region.code === regionCode), [regionCode, regions]);
  const { cities, isLoading: citiesLoading } = useCityOptions(countryCode, regionCode);

  return { regions, cities, selectedRegion, regionsLoading, citiesLoading };
}
