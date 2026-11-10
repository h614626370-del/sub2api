// @vitest-environment node
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { build } from 'vite'

describe('key configuration production bundle', () => {
  it.each([
    ['ccswitchImport', 'codexQuickConfig'],
    ['codexQuickConfig', 'ccswitchImport'],
  ])('initializes with %s imported before %s', async (first, second) => {
    const entry = fileURLToPath(new URL('./virtual-key-config-entry.ts', import.meta.url)).replace(/\\/g, '/')
    const output = await build({
      configFile: false,
      logLevel: 'silent',
      resolve: {
        alias: { '@': fileURLToPath(new URL('../../', import.meta.url)) },
      },
      plugins: [{
        name: 'key-config-test-entry',
        resolveId: (id) => id.replace(/\\/g, '/') === entry ? entry : undefined,
        load: (id) => id === entry
          ? `export * from '@/utils/${first}'; export * from '@/utils/${second}';`
          : undefined,
      }],
      build: {
        write: false,
        minify: 'esbuild',
        lib: { entry, formats: ['es'], fileName: 'key-config' },
      },
    })
    const outputs = Array.isArray(output) ? output : [output]
    const chunk = outputs.flatMap(result => 'output' in result ? result.output : [])
      .find(item => item.type === 'chunk' && item.isEntry)
    expect(chunk?.type).toBe('chunk')
    if (!chunk || chunk.type !== 'chunk') throw new Error('Missing entry chunk')
    const moduleURL = `data:text/javascript;base64,${Buffer.from(chunk.code).toString('base64')}`
    const result = spawnSync(process.execPath, ['--input-type=module', '-e', `
      const config = await import(process.argv[1]).catch(error => {
        console.error(error.name + ': ' + error.message);
        process.exit(1);
      });
      console.log(JSON.stringify({
        model: config.OPENAI_CC_SWITCH_CODEX_MODEL,
        toml: config.buildCodexQuickConfigToml({
          apiKey: 'test-key', baseUrl: 'https://example.test', platform: 'openai',
        }),
      }));
    `, moduleURL], { encoding: 'utf8', timeout: 15000 })
    expect(result.status, result.stderr).toBe(0)
    const loaded = JSON.parse(result.stdout)
    expect(loaded.model).toBe('gpt-6-astra')
    expect(loaded.toml).toContain(`model = "${loaded.model}"`)
  })
})
