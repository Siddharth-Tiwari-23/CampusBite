import { request } from './api'

export const analyticsService = {
  async queryAnalytics(question) {
    return request('/analytics/query', {
      method: 'POST',
      body: JSON.stringify({ question }),
    })
  },

  async queryTrends(period = 'THIS_WEEK') {
    return request('/analytics/trends', {
      method: 'POST',
      body: JSON.stringify({ period }),
    })
  },
}
