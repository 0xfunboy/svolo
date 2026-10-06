#!/usr/bin/env python3
"""Current ATP authoring and CLI contract, using only temporary workspaces."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
LIB=ROOT/'desktop/resources/atp/skills/atp-local-librarian/scripts/atp_local_librarian.py'
spec=importlib.util.spec_from_file_location('svolo_task_librarian',LIB)
lib=importlib.util.module_from_spec(spec);spec.loader.exec_module(lib)

class TaskPlanTests(unittest.TestCase):
    def test_schema_matches_all_runtime_skill_copies(self):
        canonical=(ROOT/'schemas/task-plan.schema.json').read_bytes()
        self.assertEqual(json.loads(canonical)['properties']['meta']['properties']['version']['enum'],['1.3','1.4'])
        for p in (ROOT/'desktop/resources/atp/skills').glob('*/references/atp-schema.json'):
            self.assertEqual(p.read_bytes(),canonical)
    def test_example_is_valid_and_inactive(self):
        graph=json.loads((ROOT/'examples/workspace-check.atp.json').read_text())
        lib.validate_graph(graph)
        self.assertEqual(graph['meta']['project_status'],'DRAFT')
    def test_cli_claim_complete_cycle(self):
        with tempfile.TemporaryDirectory(prefix='svolo-plan-contract-') as temporary:
            path=Path(temporary)/'check.atp.json';path.write_bytes((ROOT/'examples/workspace-check.atp.json').read_bytes())
            def cli(*args):
                result=subprocess.run([sys.executable,'-B',str(LIB),*args,'--plan-path',str(path)],text=True,capture_output=True,timeout=10)
                self.assertEqual(result.returncode,0,result.stderr)
                return result.stdout
            self.assertTrue(cli('atp-claim-task','--agent-id','worker').startswith('Project is not ACTIVE'))
            cli('atp-activate-project','--actor-id','user','--reason','Explicit test authority')
            self.assertTrue(cli('atp-claim-task','--agent-id','worker').startswith('TASK ASSIGNED: inspect'))
            cli('atp-complete-task','--node-id','inspect','--status','DONE','--report','Read-only contract verified')
            self.assertTrue(cli('atp-claim-task','--agent-id','worker').startswith('TASK ASSIGNED: verify'))
            cli('atp-complete-task','--node-id','verify','--status','DONE','--report','Only test fixture used')
            self.assertTrue(cli('atp-claim-task','--agent-id','worker').startswith('NO_TASKS_AVAILABLE'))
            graph=json.loads(path.read_text());self.assertTrue(all(n['status']=='COMPLETED' for n in graph['nodes'].values()))
    def test_decomposition_rejects_cycles_and_unknown_children(self):
        for tasks in [
            [{'id':'a','description':'A','dependencies':['a']}],
            [{'id':'a','description':'A','dependencies':['missing']}],
            [{'id':'a','description':'A'},{'id':'a','description':'Duplicate'}],
        ]:
            with self.assertRaises(ValueError):lib.validate_subtasks(tasks)

if __name__=='__main__': unittest.main(verbosity=2)
