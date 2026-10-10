import { describe, expect, it } from 'vitest'
import { TECH_CHIP_LIMIT, detectTechStack } from './techStack'

describe('detectTechStack', () => {
  it('finds the technologies an advertisement names, in order of appearance', () => {
    const ad = 'We build services in Golang and React on top of PostgreSQL, running on AWS with Kubernetes.'
    expect(detectTechStack(ad)).toEqual(['Golang', 'React', 'PostgreSQL', 'AWS', 'Kubernetes'])
  })

  it('handles punctuation, casing and collapsed whitespace', () => {
    expect(detectTechStack('Requirements:\n\n  TYPESCRIPT,  react.js   &   docker.')).toEqual([
      'TypeScript',
      'React',
      'Docker',
    ])
  })

  it('never reports a technology inside another word', () => {
    // The classic false positive: Java ⊂ JavaScript.
    expect(detectTechStack('Deep JavaScript experience required')).toEqual(['JavaScript'])
    expect(detectTechStack('Strong Java background')).toEqual(['Java'])
    // React ⊂ Reactive; Git ⊂ GitHub Actions.
    expect(detectTechStack('Reactive systems architect')).toEqual([])
    expect(detectTechStack('We automate with GitHub Actions')).toEqual(['GitHub Actions'])
  })

  it('refuses the ambiguous bare terms it deliberately excludes', () => {
    // "Go", "Spring" and "Express" on their own are English words: only the
    // unambiguous forms count.
    expect(detectTechStack('You will go above and beyond this spring')).toEqual([])
    // Order follows first appearance, so Express.js is reported before Spring Boot.
    expect(detectTechStack('We use Express.js and Spring Boot')).toEqual(['Express', 'Spring'])
  })

  it('does not report a technology twice', () => {
    expect(detectTechStack('Docker. Docker everywhere. More Docker.')).toEqual(['Docker'])
  })

  it('returns nothing for missing or empty text', () => {
    expect(detectTechStack(null)).toEqual([])
    expect(detectTechStack(undefined)).toEqual([])
    expect(detectTechStack('   \n  ')).toEqual([])
  })

  it('finds a stack that the sources never publish as a field', () => {
    // JobStreet publishes "Developers/Programmers" (a category) and Kalibrr's
    // skills are absent on ~91% of listings, so this is exactly the gap the
    // detector covers — from text, and labelled as detected.
    const jobstreet =
      'Develop and maintain SWITCH modules for mission-critical payment platforms. C++ and Oracle experience preferred.'
    expect(detectTechStack(jobstreet)).toEqual(['C++'])
  })

  it('keeps the chip budget small enough for a table cell', () => {
    expect(TECH_CHIP_LIMIT).toBeGreaterThan(0)
    expect(TECH_CHIP_LIMIT).toBeLessThanOrEqual(6)
  })

  // Captured verbatim from live JobStreet listings (2026-10-10). Real phrasing is
  // where the vocabulary earns its keep: this ad writes JavaScript as two words.
  it('reads a real Indonesian-language listing', () => {
    const ad =
      'Menguasai Java (Spring Boot) dan Java Script, serta memiliki pengalaman menggunakan React.js. Memiliki pemahaman yang baik mengenai database SQL.'
    expect(detectTechStack(ad)).toEqual(['Java', 'Spring', 'JavaScript', 'React', 'SQL'])
  })

  it('reads the title too, and still refuses bare "Go" in prose', () => {
    const title = 'Backend Engineer (Golang) Level Middle'
    const description =
      'Middle Software Engineer (Backend Go) building microservices in a collaborative, autonomous team.'

    // "Go" on its own is an English verb: microservices is all the text proves.
    expect(detectTechStack(description)).toEqual(['Microservices'])
    // The title's "(Golang)" is unambiguous, so the pair yields both.
    expect(detectTechStack(`${title} ${description}`)).toEqual(['Golang', 'Microservices'])
  })
})
