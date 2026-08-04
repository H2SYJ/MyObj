import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  getTagRebuildFailures,
  getTagSuggestions,
  publishGlobalDraft,
  retryTagRebuildFailure,
  rollbackGlobalRuleSet,
  saveGlobalDraft
} from './tag'

const network = vi.hoisted(() => ({
  del: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn()
}))

vi.mock('@/utils/network/request', () => network)
vi.mock('@/plugins/cache', () => ({ default: { local: { get: vi.fn() } } }))

describe('标签管理 API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('草稿保存、发布和回滚使用独立版本接口', async () => {
    network.put.mockResolvedValue({ code: 200 })
    network.post.mockResolvedValue({ code: 200 })

    await saveGlobalDraft('draft-1', 3, [{ type: 'word', pattern: '流浪地球', enabled: true }])
    await publishGlobalDraft('draft-1')
    await rollbackGlobalRuleSet('version-1')

    expect(network.put).toHaveBeenCalledWith('/admin/tag/drafts/draft-1', {
      revision: 3,
      rules: [{ type: 'word', pattern: '流浪地球', enabled: true }]
    })
    expect(network.post).toHaveBeenCalledWith('/admin/tag/drafts/draft-1/publish')
    expect(network.post).toHaveBeenCalledWith('/admin/tag/rule-sets/version-1/rollback')
  })

  it('支持查看并逐文件重试重建失败明细', async () => {
    network.get.mockResolvedValue({ code: 200 })
    network.post.mockResolvedValue({ code: 200 })

    await getTagRebuildFailures('job-1', 'failed', 25)
    await retryTagRebuildFailure('job-1', 'uf-1')

    expect(network.get).toHaveBeenCalledWith('/admin/tag/rebuild-jobs/job-1/failures', {
      status: 'failed',
      limit: 25
    })
    expect(network.post).toHaveBeenCalledWith('/admin/tag/rebuild-jobs/job-1/failures/uf-1/retry')
  })

  it('标签建议支持模糊搜索、公开范围和按ID回填', async () => {
    network.get.mockResolvedValue({ code: 200 })

    await getTagSuggestions({ keyword: '科幻', tagIds: ['tag-1', 'tag-2'], scope: 'public', limit: 2 })

    expect(network.get).toHaveBeenCalledWith('/file/tags/suggestions', {
      keyword: '科幻',
      tag_ids: 'tag-1,tag-2',
      scope: 'public',
      limit: 2
    })
  })
})
