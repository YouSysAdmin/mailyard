// The Subject a form holds, and what of it is worth sending.
//
// Both minting dialogs need these and neither owns them, so they live
// beside SubjectFields rather than in whichever dialog was written
// first.
import type { CertificateSubject } from '../../../api/certificates'

/**
 * Every field present and empty, which is what a form binds to.
 *
 * Not `{}`: a v-model against a missing key works in Vue but leaves the
 * six fields arriving in whatever order they were first typed in, and
 * the shape is worth stating once.
 */
export function blankSubject(): CertificateSubject {
  return { common_name: '', organization: '', unit: '', country: '', state: '', locality: '' }
}

/**
 * Only the fields somebody actually filled in, or undefined for none.
 *
 * An empty string would encode as a present-but-empty attribute in the
 * certificate rather than an absent one. The server drops those anyway -
 * but sending them is asking it to store a blank Organization, and a
 * Subject of six empty attributes is not the same object as no Subject.
 */
export function filledSubject(s: CertificateSubject): CertificateSubject | undefined {
  const out: CertificateSubject = {}
  let any = false
  for (const [k, v] of Object.entries(s)) {
    if (typeof v === 'string' && v.trim() !== '') {
      out[k as keyof CertificateSubject] = v.trim()
      any = true
    }
  }

  return any ? out : undefined
}
