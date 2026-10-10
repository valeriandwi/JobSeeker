/**
 * Tech detection in listing TEXT — a reading aid, NOT a field of record.
 *
 * WHY THIS IS DERIVED AND SAYS SO: the sources do not publish a usable tech
 * stack. Kalibrr carries `job_sds_skills` on ~9% of listings and mixes
 * technologies with soft skills ("Persuasion and Negotiation", "Taxation");
 * JobStreet publishes occupational categories ("Developers/Programmers"), not
 * technologies. Rather than dress a category up as a stack, this scans the title
 * and description against a bounded vocabulary and the UI labels the result
 * "detected". It is never merged into `attributes_verified` and never treated as
 * something the source stated.
 *
 * PRECISION RULES (each one exists because the naive version lied):
 *  - Word boundaries on both sides, so "Java" does not match "JavaScript" and
 *    "React" does not match "Reactive".
 *  - Ambiguous bare terms are deliberately absent: plain "Go" (an English verb),
 *    "Spring" (a season), "Express" (a verb), "R"/"C" (single letters). Their
 *    unambiguous forms are used instead ("Golang", "Spring Boot", "Express.js").
 *  - No stemming or fuzzy matching: a miss is cheaper than a false claim.
 */

type Entry = { label: string; aliases: readonly string[] }

/** Bounded vocabulary: canonical label + the spellings that actually appear. */
const VOCABULARY: readonly Entry[] = [
  // Languages
  { label: 'TypeScript', aliases: ['TypeScript'] },
  { label: 'JavaScript', aliases: ['JavaScript', 'Java Script', 'ECMAScript'] },
  { label: 'Java', aliases: ['Java'] },
  { label: 'Kotlin', aliases: ['Kotlin'] },
  { label: 'Swift', aliases: ['Swift'] },
  { label: 'Golang', aliases: ['Golang', 'Go lang', 'GoLang'] },
  { label: 'Rust', aliases: ['Rust'] },
  { label: 'Ruby', aliases: ['Ruby'] },
  { label: 'PHP', aliases: ['PHP'] },
  { label: 'Python', aliases: ['Python'] },
  { label: 'C#', aliases: ['C#', 'C-Sharp', 'CSharp'] },
  { label: 'C++', aliases: ['C++', 'CPP'] },
  { label: 'Scala', aliases: ['Scala'] },
  { label: 'Elixir', aliases: ['Elixir'] },
  { label: 'Dart', aliases: ['Dart'] },
  { label: 'SQL', aliases: ['SQL'] },
  { label: 'Bash', aliases: ['Bash', 'Shell Scripting'] },
  { label: 'Objective-C', aliases: ['Objective-C', 'Objective C'] },
  { label: 'Visual Basic', aliases: ['Visual Basic', 'VB.NET', 'VBA'] },

  // Frontend
  { label: 'React', aliases: ['React', 'React.js', 'ReactJS'] },
  { label: 'React Native', aliases: ['React Native'] },
  { label: 'Next.js', aliases: ['Next.js', 'NextJS'] },
  { label: 'Vue', aliases: ['Vue', 'Vue.js', 'VueJS'] },
  { label: 'Angular', aliases: ['Angular', 'AngularJS'] },
  { label: 'Svelte', aliases: ['Svelte', 'SvelteKit'] },
  { label: 'Tailwind', aliases: ['Tailwind', 'TailwindCSS', 'Tailwind CSS'] },
  { label: 'HTML', aliases: ['HTML', 'HTML5'] },
  { label: 'CSS', aliases: ['CSS', 'CSS3'] },
  { label: 'Sass', aliases: ['Sass', 'SCSS'] },
  { label: 'Redux', aliases: ['Redux'] },
  { label: 'Webpack', aliases: ['Webpack'] },
  { label: 'Vite', aliases: ['Vite', 'ViteJS'] },
  { label: 'Storybook', aliases: ['Storybook'] },

  // Backend / platform
  { label: 'Node.js', aliases: ['Node.js', 'NodeJS'] },
  { label: 'Express', aliases: ['Express.js', 'ExpressJS'] },
  { label: 'NestJS', aliases: ['NestJS', 'Nest.js'] },
  { label: 'Django', aliases: ['Django'] },
  { label: 'Flask', aliases: ['Flask'] },
  { label: 'FastAPI', aliases: ['FastAPI'] },
  { label: 'Spring', aliases: ['Spring Boot', 'Spring Framework', 'SpringBoot'] },
  { label: 'Laravel', aliases: ['Laravel'] },
  { label: '.NET', aliases: ['.NET', 'DotNet', 'ASP.NET'] },
  { label: 'Rails', aliases: ['Ruby on Rails', 'Rails'] },
  { label: 'GraphQL', aliases: ['GraphQL'] },
  { label: 'REST API', aliases: ['REST API', 'RESTful', 'REST APIs'] },
  { label: 'gRPC', aliases: ['gRPC'] },
  { label: 'Microservices', aliases: ['Microservices', 'Microservice'] },

  // Data stores & messaging
  { label: 'PostgreSQL', aliases: ['PostgreSQL', 'Postgres'] },
  { label: 'MySQL', aliases: ['MySQL'] },
  { label: 'SQL Server', aliases: ['SQL Server', 'MSSQL'] },
  { label: 'MongoDB', aliases: ['MongoDB', 'Mongo'] },
  { label: 'Redis', aliases: ['Redis'] },
  { label: 'Elasticsearch', aliases: ['Elasticsearch', 'Elastic Search'] },
  { label: 'DynamoDB', aliases: ['DynamoDB'] },
  { label: 'Cassandra', aliases: ['Cassandra'] },
  { label: 'SQLite', aliases: ['SQLite'] },
  { label: 'Kafka', aliases: ['Kafka'] },
  { label: 'RabbitMQ', aliases: ['RabbitMQ'] },

  // Cloud, infra, delivery
  { label: 'AWS', aliases: ['AWS', 'Amazon Web Services'] },
  { label: 'GCP', aliases: ['GCP', 'Google Cloud'] },
  { label: 'Azure', aliases: ['Azure', 'Microsoft Azure'] },
  { label: 'Docker', aliases: ['Docker'] },
  { label: 'Kubernetes', aliases: ['Kubernetes', 'K8s'] },
  { label: 'Terraform', aliases: ['Terraform'] },
  { label: 'Ansible', aliases: ['Ansible'] },
  { label: 'Jenkins', aliases: ['Jenkins'] },
  { label: 'GitHub Actions', aliases: ['GitHub Actions'] },
  { label: 'GitLab CI', aliases: ['GitLab CI', 'GitLab CI/CD'] },
  { label: 'CI/CD', aliases: ['CI/CD', 'CICD', 'Continuous Integration'] },
  { label: 'Linux', aliases: ['Linux', 'Unix'] },
  { label: 'Nginx', aliases: ['Nginx'] },
  { label: 'Serverless', aliases: ['Serverless', 'AWS Lambda', 'Lambda Function'] },
  { label: 'Prometheus', aliases: ['Prometheus'] },
  { label: 'Grafana', aliases: ['Grafana'] },

  // Data & ML
  { label: 'Pandas', aliases: ['Pandas'] },
  { label: 'NumPy', aliases: ['NumPy'] },
  { label: 'PyTorch', aliases: ['PyTorch'] },
  { label: 'TensorFlow', aliases: ['TensorFlow'] },
  { label: 'scikit-learn', aliases: ['scikit-learn', 'sklearn'] },
  { label: 'Spark', aliases: ['Apache Spark', 'PySpark'] },
  { label: 'Machine Learning', aliases: ['Machine Learning', 'ML Model', 'ML Models'] },
  { label: 'Power BI', aliases: ['Power BI', 'PowerBI'] },
  { label: 'Tableau', aliases: ['Tableau'] },
  { label: 'Airflow', aliases: ['Airflow', 'Apache Airflow'] },

  // Mobile
  { label: 'Flutter', aliases: ['Flutter'] },
  { label: 'Android', aliases: ['Android'] },
  { label: 'iOS', aliases: ['iOS'] },

  // Testing & tooling
  { label: 'Jest', aliases: ['Jest'] },
  { label: 'Playwright', aliases: ['Playwright'] },
  { label: 'Cypress', aliases: ['Cypress'] },
  { label: 'Selenium', aliases: ['Selenium'] },
  { label: 'Pytest', aliases: ['Pytest', 'PyTest'] },
  { label: 'JUnit', aliases: ['JUnit'] },
  { label: 'Postman', aliases: ['Postman'] },
  { label: 'Git', aliases: ['Git'] },
  { label: 'Jira', aliases: ['Jira'] },
  { label: 'Figma', aliases: ['Figma'] },
  { label: 'Agile', aliases: ['Agile'] },
  { label: 'Scrum', aliases: ['Scrum'] },
]

function escapeTerm(term: string): string {
  return term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// Boundaries are explicit non-alphanumeric lookarounds rather than \b: it keeps
// "C++", "C#", "Node.js" and "CI/CD" working while still refusing "Java" inside
// "JavaScript".
const PATTERNS: readonly { label: string; re: RegExp }[] = VOCABULARY.map((entry) => ({
  label: entry.label,
  re: new RegExp(
    `(?<![A-Za-z0-9])(${entry.aliases.map(escapeTerm).join('|')})(?![A-Za-z0-9])`,
    'i',
  ),
}))

/**
 * Technologies named in the supplied text, in order of first appearance.
 * Returns [] when nothing matches — callers render an explicit "none detected"
 * rather than an empty cell.
 */
export function detectTechStack(text: string | null | undefined): string[] {
  const haystack = (text ?? '').replace(/\s+/g, ' ').trim()
  if (!haystack) return []

  const found: { label: string; at: number }[] = []
  for (const { label, re } of PATTERNS) {
    const at = haystack.search(re)
    if (at >= 0) found.push({ label, at })
  }
  return found.sort((a, b) => a.at - b.at).map((f) => f.label)
}

/** How many chips a table cell shows before collapsing into "+N". */
export const TECH_CHIP_LIMIT = 4
