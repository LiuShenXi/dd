import { apiClient } from './client'
import type { CarpoolBoostStatus, CarpoolDetails } from '@/types/carpool'

export async function getDetails(): Promise<CarpoolDetails> {
  const { data } = await apiClient.get<CarpoolDetails>('/user/carpool/details')
  return data
}

export async function getBoosts(): Promise<CarpoolBoostStatus> {
  const { data } = await apiClient.get<CarpoolBoostStatus>('/user/carpool/boosts')
  return data
}

export async function claimBoost(idempotencyKey: string): Promise<CarpoolBoostStatus> {
  const { data } = await apiClient.post<CarpoolBoostStatus>(
    '/user/carpool/boosts',
    {},
    { headers: { 'Idempotency-Key': idempotencyKey } }
  )
  return data
}

export const carpoolAPI = { getDetails, getBoosts, claimBoost }

export default carpoolAPI
