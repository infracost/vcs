<h3>💰 Infracost report</h3>

<p>Consider fixing these issues, they don't align with your company's FinOps policies & the Well-Architected Framework. <b>Add a PR comment with <code>@infracost help</code> to see how you can dismiss or snooze issues and unblock your PR.</b></p>

<table>
  <tr><td colspan="2" width="1000px">Tagging policies</td></tr>
  
<tr><td colspan="2" title="Blocking failure">
  <details open>
    <summary>
    <b>❌ Require env tag</b>
  </summary><br/>

All resources must have an env tag.
  </details>
</td></tr>
<tr><td></td><td>

resource [aws_instance.web](https://github.com/my-org/my-repo/blob/abc123/main.tf#L10)
  * Missing mandatory tag `env`
    in project `my-project`
    </td></tr>
<tr><td></td><td>

resource [aws_s3_bucket.data](https://github.com/my-org/my-repo/blob/abc123/storage.tf#L5)
  * Tag `env=prod` does not match pattern `/^(production|staging|dev)$/`
    in projects `my-project`, `other-project`
    </td></tr>

  </table>

<sub>
  This comment will be updated when code changes.
</sub>

