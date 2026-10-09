import { http } from './client'
import type { Resume, ResumeUploadResult } from './types'

export const resumesApi = {
  list: () => http.get<{ resumes: Resume[] }>('/api/v1/resumes'),
  upload: (file: File) => {
    const fd = new FormData()
    fd.append('file', file)
    return http.upload<ResumeUploadResult>('/api/v1/resumes', fd)
  },
  remove: (id: string) => http.del<void>(`/api/v1/resumes/${id}`),
}
