import { describe, expect, it } from 'vitest'
import { mountWithPlugins } from '@/test/mountWithPlugins'
import MarkdownEditor from '../MarkdownEditor.vue'

const FILE = '---\nid: "US-10"\ntitulo: "Somar"\n---\n# US-10\n\nTexto **forte**.\n'

function mountEditor(initial: string) {
  const wrapper = mountWithPlugins(MarkdownEditor, {
    props: {
      modelValue: initial,
      'onUpdate:modelValue': (value: string) => wrapper.setProps({ modelValue: value }),
      id: 'content',
    },
    attachTo: document.body,
  })
  return wrapper
}

describe('MarkdownEditor', () => {
  it('US-53: keeps the attributes of the parent on the textarea, with the file as text', () => {
    const wrapper = mountEditor(FILE)
    const area = wrapper.find('textarea')
    expect(area.attributes('id')).toBe('content')
    expect((area.element as HTMLTextAreaElement).value).toBe(FILE)
    wrapper.unmount()
  })

  it('wraps the selection in bold from the toolbar', async () => {
    const wrapper = mountEditor('Texto forte')
    const area = wrapper.find('textarea').element as HTMLTextAreaElement
    area.setSelectionRange(6, 11)
    await wrapper.find('button[aria-label="Negrito (Ctrl+B)"]').trigger('click')
    expect(wrapper.props('modelValue')).toBe('Texto **forte**')
    wrapper.unmount()
  })

  it('turns the lines of the selection into a list', async () => {
    const wrapper = mountEditor('um\ndois')
    const area = wrapper.find('textarea').element as HTMLTextAreaElement
    area.setSelectionRange(0, 7)
    await wrapper.find('button[aria-label="Lista"]').trigger('click')
    expect(wrapper.props('modelValue')).toBe('- um\n- dois')
    wrapper.unmount()
  })

  it('RNF-05: previews the body through the sanitizer, with the YAML header apart', async () => {
    const wrapper = mountEditor(FILE.replace('Texto', 'Texto <script>alert(1)</script>'))
    await wrapper.find('[data-testid="markdown-editor-preview"]').trigger('click')
    const rendered = wrapper.find('[data-testid="markdown-editor-rendered"]')
    expect(rendered.find('strong').text()).toBe('forte')
    expect(rendered.find('script').exists()).toBe(false)
    expect(rendered.find('details pre').text()).toContain('titulo: "Somar"')
    expect(rendered.text()).not.toContain('---')
    wrapper.unmount()
  })
})
