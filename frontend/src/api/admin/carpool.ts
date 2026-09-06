import { apiClient } from '../client'
import type {
  CarpoolAdminCycle,
  CarpoolAdminTerm,
  CarpoolBucket,
  CarpoolBillingException,
  CarpoolBillingReconcileRequest,
  CarpoolLedgerEntry,
  CarpoolListParams,
  CarpoolListResponse,
  CarpoolOpenTermRequest,
  CarpoolPaymentInput,
  CarpoolPaymentRecord,
  CarpoolPlan,
  CarpoolPlanVersionRequest,
  CarpoolPreview,
  CarpoolPreviewRequest,
  CarpoolRenewTermRequest,
  CarpoolResetBatch,
  CarpoolResetObservation,
  CarpoolResetScanResult,
} from '@/types/carpool'

const idempotencyHeaders = (key: string) => ({ 'Idempotency-Key': key })

type NullableCarpoolListResponse<T> = Omit<CarpoolListResponse<T>, 'items'> & { items: T[] | null }

function normalizeListResponse<T>(data: NullableCarpoolListResponse<T>): CarpoolListResponse<T> {
  return { ...data, items: Array.isArray(data.items) ? data.items : [] }
}

export async function listPlans(): Promise<CarpoolPlan[]> {
  const { data } = await apiClient.get<CarpoolPlan[] | null>('/admin/carpool/plans')
  return Array.isArray(data) ? data : []
}

export async function createPlanVersion(planId: number, input: CarpoolPlanVersionRequest, key: string): Promise<CarpoolPlan> {
  const { data } = await apiClient.post<CarpoolPlan>(`/admin/carpool/plans/${planId}/versions`, input, {
    headers: idempotencyHeaders(key),
  })
  return data
}

export async function previewTerm(userId: number, input: CarpoolPreviewRequest): Promise<CarpoolPreview> {
  const { data } = await apiClient.post<CarpoolPreview>(`/admin/users/${userId}/carpool/preview`, input)
  return data
}

export async function openTerm(userId: number, input: CarpoolOpenTermRequest, key: string): Promise<CarpoolAdminTerm> {
  const { data } = await apiClient.post<CarpoolAdminTerm>(`/admin/users/${userId}/carpool/terms`, input, {
    headers: idempotencyHeaders(key),
  })
  return data
}

export async function renewTerm(termId: number, input: CarpoolRenewTermRequest, key: string): Promise<CarpoolAdminTerm> {
  const { data } = await apiClient.post<CarpoolAdminTerm>(`/admin/carpool/terms/${termId}/renew`, input, {
    headers: idempotencyHeaders(key),
  })
  return data
}

export async function terminateTerm(termId: number, reason: string, key: string): Promise<CarpoolAdminTerm> {
  const { data } = await apiClient.post<CarpoolAdminTerm>(
    `/admin/carpool/terms/${termId}/terminate`,
    { reason, effective_at: null },
    { headers: idempotencyHeaders(key) }
  )
  return data
}

export async function listTerms(params: CarpoolListParams = {}): Promise<CarpoolListResponse<CarpoolAdminTerm>> {
  const { data } = await apiClient.get<NullableCarpoolListResponse<CarpoolAdminTerm>>('/admin/carpool/terms', { params })
  return normalizeListResponse(data)
}

export async function listCycles(params: CarpoolListParams = {}): Promise<CarpoolListResponse<CarpoolAdminCycle>> {
  const { data } = await apiClient.get<NullableCarpoolListResponse<CarpoolAdminCycle>>('/admin/carpool/cycles', { params })
  return normalizeListResponse(data)
}

export async function listLedger(params: CarpoolListParams = {}): Promise<CarpoolListResponse<CarpoolLedgerEntry>> {
  const { data } = await apiClient.get<NullableCarpoolListResponse<CarpoolLedgerEntry>>('/admin/carpool/ledger', { params })
  return normalizeListResponse(data)
}

export async function listBillingExceptions(params: CarpoolListParams = {}): Promise<CarpoolListResponse<CarpoolBillingException>> {
  const { data } = await apiClient.get<NullableCarpoolListResponse<CarpoolBillingException>>('/admin/carpool/billing-exceptions', { params })
  return normalizeListResponse(data)
}

export async function reconcileBillingException(id: number, input: CarpoolBillingReconcileRequest, key: string): Promise<CarpoolBillingException> {
  const { data } = await apiClient.post<CarpoolBillingException>(`/admin/carpool/billing-exceptions/${id}/reconcile`, input, {
    headers: idempotencyHeaders(key),
  })
  return data
}

export async function listPayments(termId: number, params: CarpoolListParams = {}): Promise<CarpoolListResponse<CarpoolPaymentRecord>> {
  const { data } = await apiClient.get<NullableCarpoolListResponse<CarpoolPaymentRecord>>(
    `/admin/carpool/terms/${termId}/payments`,
    { params }
  )
  return normalizeListResponse(data)
}

export async function addPayment(termId: number, input: CarpoolPaymentInput, key: string): Promise<CarpoolPaymentRecord> {
  const { data } = await apiClient.post<CarpoolPaymentRecord>(
    `/admin/carpool/terms/${termId}/payments`,
    input,
    { headers: idempotencyHeaders(key) }
  )
  return data
}

export async function adjustCycle(
  cycleId: number,
  input: { bucket: CarpoolBucket; delta_usd: string; reason: string; reverses_ledger_id: number | null },
  key: string
): Promise<CarpoolLedgerEntry[]> {
  const { data } = await apiClient.post<CarpoolLedgerEntry[]>(
    `/admin/carpool/cycles/${cycleId}/adjustments`,
    input,
    { headers: idempotencyHeaders(key) }
  )
  return data
}

export async function listResetBatches(params: CarpoolListParams = {}): Promise<CarpoolListResponse<CarpoolResetBatch>> {
  const { data } = await apiClient.get<NullableCarpoolListResponse<CarpoolResetBatch>>('/admin/carpool/reset-batches', { params })
  return normalizeListResponse(data)
}

export async function registerResetQualification(
  input: { scope_id: 1; confirmed: true; source_event_key: string; reason: string },
  key: string
): Promise<CarpoolResetBatch> {
  const { data } = await apiClient.post<CarpoolResetBatch>('/admin/carpool/reset-batches', input, {
    headers: idempotencyHeaders(key),
  })
  return data
}

export async function scheduleResetBatch(batchId: number, reason: string, key: string): Promise<CarpoolResetBatch> {
  const { data } = await apiClient.post<CarpoolResetBatch>(
    `/admin/carpool/reset-batches/${batchId}/schedule`,
    { reason },
    { headers: idempotencyHeaders(key) }
  )
  return data
}

export async function executeResetBatch(batchId: number, key: string): Promise<CarpoolResetBatch> {
  const { data } = await apiClient.post<CarpoolResetBatch>(
    `/admin/carpool/reset-batches/${batchId}/execute`,
    {},
    { headers: idempotencyHeaders(key) }
  )
  return data
}

export async function listResetObservations(params: CarpoolListParams = {}): Promise<CarpoolListResponse<CarpoolResetObservation>> {
  const { data } = await apiClient.get<NullableCarpoolListResponse<CarpoolResetObservation>>('/admin/carpool/reset-observations', { params })
  return normalizeListResponse(data)
}

export async function scanResetObservations(key: string): Promise<CarpoolResetScanResult> {
  const { data } = await apiClient.post<CarpoolResetScanResult>(
    '/admin/carpool/reset-observations/scan',
    {},
    { headers: idempotencyHeaders(key) }
  )
  return data
}

export const carpoolAdminAPI = {
  listPlans,
  createPlanVersion,
  previewTerm,
  openTerm,
  renewTerm,
  terminateTerm,
  listTerms,
  listCycles,
  listLedger,
  listBillingExceptions,
  reconcileBillingException,
  listPayments,
  addPayment,
  adjustCycle,
  listResetBatches,
  registerResetQualification,
  scheduleResetBatch,
  executeResetBatch,
  listResetObservations,
  scanResetObservations,
}

export default carpoolAdminAPI
