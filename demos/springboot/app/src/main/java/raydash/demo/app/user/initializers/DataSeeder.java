package raydash.demo.app.user.initializers;

import java.util.List;

import org.springframework.boot.CommandLineRunner;
import org.springframework.stereotype.Component;

import raydash.demo.app.user.models.UserEntity;
import raydash.demo.app.user.repositories.UserRepository;

@Component
public class DataSeeder implements CommandLineRunner {
    private final UserRepository userRepositoryObject;

    public DataSeeder(UserRepository userRepositoryObject) {
        this.userRepositoryObject = userRepositoryObject;
    }

    @Override
    public void run(String... args) throws Exception {
        if(userRepositoryObject.count() == 0) {
            System.out.println("--> Demo database is empty. Seeding test data...");

            userRepositoryObject.saveAll(List.of(
                UserEntity.builder().userId(1L).name("AA AAA").email("123@abc.com").build(),
                UserEntity.builder().userId(2L).name("BB BBB").email("456@def.com").build(),
                UserEntity.builder().userId(3L).name("CC CCC").email("789@ijk.com").build(),
                UserEntity.builder().userId(4L).name("DD DDD").email("999@lmn.com").build(),
                UserEntity.builder().userId(5L).name("EE EEE").email("333@opq.com").build()
            ));

            System.out.println("--> Demo database seeding complete. Total records: " + userRepositoryObject.count());
        } else {
            System.out.println("--> Demo database already contains data. Skipping seed.");
        }
    }
    
}
